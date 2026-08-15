package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

const (
	githubOAuthFlowTTL       = 10 * time.Minute
	githubPendingIdentityTTL = 30 * time.Minute

	githubOAuthStateKey     = "oauth_github_state"
	githubOAuthVerifierKey  = "oauth_github_verifier"
	githubOAuthStartedAtKey = "oauth_github_started_at"
	githubOAuthModeKey      = "oauth_github_mode"
	githubOAuthNextKey      = "oauth_github_next"

	githubPendingSubjectKey       = "oauth_github_pending_subject"
	githubPendingUsernameKey      = "oauth_github_pending_username"
	githubPendingEmailKey         = "oauth_github_pending_email"
	githubPendingEmailVerifiedKey = "oauth_github_pending_email_verified"
	githubPendingAvatarKey        = "oauth_github_pending_avatar"
	githubPendingStartedAtKey     = "oauth_github_pending_started_at"
	githubPendingNextKey          = "oauth_github_pending_next"
	githubPendingCSRFKey          = "oauth_github_pending_csrf"

	authMethodsCSRFKey = "auth_methods_csrf"
)

type githubPendingIdentity struct {
	Identity  *types.PersonOAuthIdentity
	StartedAt time.Time
	Next      string
	CSRF      string
}

type GitHubOAuthConfirmPage struct {
	Speaker  *types.Speaker
	Identity *types.PersonOAuthIdentity
	CSRF     string
	Year     uint
}

func GitHubOAuthStart(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	provider := auth.NewGitHubOAuthProvider(ctx.Env)
	if !provider.Enabled() {
		redirectGitHubOAuthError(w, r, ctx, "GitHub sign-in is not configured yet.")
		return
	}

	state, err := randomAuthToken()
	if err != nil {
		ctx.Err.Printf("GitHub OAuth state: %s", err)
		redirectGitHubOAuthError(w, r, ctx, "Unable to start GitHub sign-in. Try again.")
		return
	}
	verifier, err := randomAuthToken()
	if err != nil {
		ctx.Err.Printf("GitHub OAuth verifier: %s", err)
		redirectGitHubOAuthError(w, r, ctx, "Unable to start GitHub sign-in. Try again.")
		return
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])

	viewer := auth.RequireOptional(r, ctx)
	mode := "login"
	if viewer != nil {
		mode = "link"
	}
	next := auth.SafeNext(r.URL.Query().Get("next"), "/dashboard")
	clearGitHubOAuthSession(ctx, r)
	clearPendingGitHubIdentity(ctx, r)
	ctx.Session.Put(r.Context(), githubOAuthStateKey, state)
	ctx.Session.Put(r.Context(), githubOAuthVerifierKey, verifier)
	ctx.Session.Put(r.Context(), githubOAuthStartedAtKey, time.Now().UTC().Format(time.RFC3339Nano))
	ctx.Session.Put(r.Context(), githubOAuthModeKey, mode)
	ctx.Session.Put(r.Context(), githubOAuthNextKey, next)

	authorizationURL, err := provider.AuthorizationURL(state, challenge)
	if err != nil {
		ctx.Err.Printf("GitHub OAuth authorization URL: %s", err)
		clearGitHubOAuthSession(ctx, r)
		redirectGitHubOAuthError(w, r, ctx, "Unable to start GitHub sign-in. Try again.")
		return
	}
	http.Redirect(w, r, authorizationURL, http.StatusFound)
}

func GitHubOAuthCallback(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	provider := auth.NewGitHubOAuthProvider(ctx.Env)
	if !provider.Enabled() {
		redirectGitHubOAuthError(w, r, ctx, "GitHub sign-in is not configured yet.")
		return
	}

	expectedState := ctx.Session.GetString(r.Context(), githubOAuthStateKey)
	verifier := ctx.Session.GetString(r.Context(), githubOAuthVerifierKey)
	startedAtRaw := ctx.Session.GetString(r.Context(), githubOAuthStartedAtKey)
	mode := ctx.Session.GetString(r.Context(), githubOAuthModeKey)
	next := auth.SafeNext(ctx.Session.GetString(r.Context(), githubOAuthNextKey), "/dashboard")
	clearGitHubOAuthSession(ctx, r)

	startedAt, startedAtErr := time.Parse(time.RFC3339Nano, startedAtRaw)
	flowAge := time.Since(startedAt)
	if expectedState == "" || verifier == "" || startedAtErr != nil || flowAge < -time.Minute || flowAge > githubOAuthFlowTTL ||
		!secureTokenEqual(expectedState, r.URL.Query().Get("state")) {
		redirectGitHubOAuthError(w, r, ctx, "That GitHub sign-in attempt expired or was already used. Try again.")
		return
	}
	if providerError := strings.TrimSpace(r.URL.Query().Get("error")); providerError != "" {
		redirectGitHubOAuthError(w, r, ctx, "GitHub sign-in was cancelled.")
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		redirectGitHubOAuthError(w, r, ctx, "GitHub did not return a sign-in code. Try again.")
		return
	}

	token, err := provider.Exchange(r.Context(), code, verifier)
	if err != nil {
		ctx.Err.Printf("GitHub OAuth exchange: %s", err)
		redirectGitHubOAuthError(w, r, ctx, "GitHub sign-in could not be verified. Try again.")
		return
	}
	providerIdentity, err := provider.FetchIdentity(r.Context(), token)
	if err != nil {
		ctx.Err.Printf("GitHub OAuth identity: %s", err)
		redirectGitHubOAuthError(w, r, ctx, "GitHub account details could not be loaded. Try again.")
		return
	}

	linked, err := getters.FindOAuthIdentity(ctx, providerIdentity.Provider, providerIdentity.Subject)
	if err != nil {
		ctx.Err.Printf("GitHub OAuth linked identity lookup: %s", err)
		redirectGitHubOAuthError(w, r, ctx, "GitHub sign-in could not be completed. Try again.")
		return
	}
	viewer := auth.RequireOptional(r, ctx)
	if linked != nil {
		if viewer != nil && viewer.PersonID != linked.PersonID {
			recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodGitHub), "oauth_link_conflict", map[string]any{"provider_subject": providerIdentity.Subject})
			redirectGitHubOAuthError(w, r, ctx, "That GitHub account is already linked to another bitcoin++ profile.")
			return
		}
		providerIdentity.PersonID = linked.PersonID
		providerIdentity, err = getters.LinkOAuthIdentity(ctx, linked.PersonID, providerIdentity)
		if err != nil {
			ctx.Err.Printf("GitHub OAuth refresh linked identity: %s", err)
			redirectGitHubOAuthError(w, r, ctx, "GitHub sign-in could not be completed. Try again.")
			return
		}
		if err := getters.MarkOAuthIdentityLogin(ctx, providerIdentity.ID); err != nil {
			ctx.Err.Printf("GitHub OAuth last login: %s", err)
		}
		if err := auth.LoginPerson(ctx, r, linked.PersonID, auth.MethodGitHub); err != nil {
			ctx.Err.Printf("GitHub OAuth session: %s", err)
			redirectGitHubOAuthError(w, r, ctx, "GitHub was verified, but the session could not be started. Try again.")
			return
		}
		recordAuthAudit(ctx, r, linked.PersonID, string(auth.MethodGitHub), "login_succeeded", map[string]any{"oauth_identity_id": providerIdentity.ID})
		if mode == "link" {
			next = "/dashboard/emails?flash=" + url.QueryEscape("GitHub @"+providerIdentity.Username+" is already linked.")
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}

	if err := storePendingGitHubIdentity(ctx, r, providerIdentity, next); err != nil {
		ctx.Err.Printf("GitHub OAuth pending identity: %s", err)
		redirectGitHubOAuthError(w, r, ctx, "GitHub sign-in could not be completed. Try again.")
		return
	}
	if viewer == nil {
		destination := "/login?next=" + url.QueryEscape("/auth/oauth/github/confirm") +
			"&flash=" + url.QueryEscape("GitHub @"+providerIdentity.Username+" was verified. Sign in by email once to choose the bitcoin++ profile to link.")
		http.Redirect(w, r, destination, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/auth/oauth/github/confirm", http.StatusSeeOther)
}

func GitHubOAuthConfirm(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requireGitHubLinkPerson(w, r, ctx)
	if viewer == nil {
		return
	}
	pending, err := pendingGitHubIdentity(ctx, r)
	if err != nil {
		clearPendingGitHubIdentity(ctx, r)
		redirectPersonEmails(w, r, "", "That GitHub link attempt expired. Start again.")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "oauth_github_confirm.tmpl", &GitHubOAuthConfirmPage{
		Speaker: viewer.Speaker, Identity: pending.Identity, CSRF: pending.CSRF, Year: helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("GitHub OAuth confirmation render: %s", err)
		http.Error(w, "Unable to render GitHub confirmation", http.StatusInternalServerError)
	}
}

func GitHubOAuthConfirmAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requireGitHubLinkPerson(w, r, ctx)
	if viewer == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		redirectPersonEmails(w, r, "", "Invalid GitHub confirmation.")
		return
	}
	pending, err := pendingGitHubIdentity(ctx, r)
	if err != nil || !secureTokenEqual(pending.CSRF, r.FormValue("csrf")) {
		clearPendingGitHubIdentity(ctx, r)
		recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodGitHub), "oauth_link_confirmation_rejected", nil)
		redirectPersonEmails(w, r, "", "That GitHub confirmation expired or was already used. Start again.")
		return
	}
	clearPendingGitHubIdentity(ctx, r)

	linked, err := getters.LinkOAuthIdentity(ctx, viewer.PersonID, pending.Identity)
	if errors.Is(err, getters.ErrOAuthIdentityLinked) {
		recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodGitHub), "oauth_link_conflict", map[string]any{"provider_subject": pending.Identity.Subject})
		redirectPersonEmails(w, r, "", "That GitHub account is already linked to another bitcoin++ profile.")
		return
	}
	if err != nil {
		ctx.Err.Printf("GitHub OAuth link: %s", err)
		redirectPersonEmails(w, r, "", "GitHub could not be linked. Try again.")
		return
	}
	if err := getters.MarkOAuthIdentityLogin(ctx, linked.ID); err != nil {
		ctx.Err.Printf("GitHub OAuth linked login time: %s", err)
	}
	if err := auth.LoginPerson(ctx, r, viewer.PersonID, auth.MethodGitHub); err != nil {
		ctx.Err.Printf("GitHub OAuth linked session: %s", err)
		redirectPersonEmails(w, r, "", "GitHub was linked, but the session could not be refreshed.")
		return
	}
	recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodGitHub), "oauth_identity_linked", map[string]any{"oauth_identity_id": linked.ID})
	destination := auth.SafeNext(pending.Next, "/dashboard/emails")
	if destination == "/dashboard" {
		destination = "/dashboard/emails"
	}
	separator := "?"
	if strings.Contains(destination, "?") {
		separator = "&"
	}
	http.Redirect(w, r, destination+separator+"flash="+url.QueryEscape("GitHub @"+linked.Username+" is now linked."), http.StatusSeeOther)
}

func GitHubOAuthUnlink(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requirePersonIdentity(w, r, ctx)
	if viewer == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodGitHub), "oauth_unlink_rejected", nil)
		redirectPersonEmails(w, r, "", "That unlink request expired. Reload the page and try again.")
		return
	}
	identity, err := getters.UnlinkOAuthIdentity(ctx, viewer.PersonID, strings.TrimSpace(r.FormValue("identity_id")))
	if err != nil {
		ctx.Err.Printf("GitHub OAuth unlink: %s", err)
		redirectPersonEmails(w, r, "", "GitHub could not be unlinked. Try again.")
		return
	}
	if identity == nil {
		redirectPersonEmails(w, r, "", "That GitHub identity is no longer linked.")
		return
	}
	recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodGitHub), "oauth_identity_unlinked", map[string]any{"provider_subject": identity.Subject})
	redirectPersonEmails(w, r, "GitHub @"+identity.Username+" was unlinked.", "")
}

func ensureAuthMethodsCSRF(ctx *config.AppContext, r *http.Request) (string, error) {
	if token := ctx.Session.GetString(r.Context(), authMethodsCSRFKey); token != "" {
		return token, nil
	}
	token, err := randomAuthToken()
	if err != nil {
		return "", err
	}
	ctx.Session.Put(r.Context(), authMethodsCSRFKey, token)
	return token, nil
}

func storePendingGitHubIdentity(ctx *config.AppContext, r *http.Request, identity *types.PersonOAuthIdentity, next string) error {
	csrf, err := randomAuthToken()
	if err != nil {
		return err
	}
	clearPendingGitHubIdentity(ctx, r)
	ctx.Session.Put(r.Context(), githubPendingSubjectKey, identity.Subject)
	ctx.Session.Put(r.Context(), githubPendingUsernameKey, identity.Username)
	ctx.Session.Put(r.Context(), githubPendingEmailKey, identity.Email)
	ctx.Session.Put(r.Context(), githubPendingEmailVerifiedKey, identity.EmailVerified)
	ctx.Session.Put(r.Context(), githubPendingAvatarKey, identity.AvatarURL)
	ctx.Session.Put(r.Context(), githubPendingStartedAtKey, time.Now().UTC().Format(time.RFC3339Nano))
	ctx.Session.Put(r.Context(), githubPendingNextKey, auth.SafeNext(next, "/dashboard"))
	ctx.Session.Put(r.Context(), githubPendingCSRFKey, csrf)
	return nil
}

func pendingGitHubIdentity(ctx *config.AppContext, r *http.Request) (*githubPendingIdentity, error) {
	startedAt, err := time.Parse(time.RFC3339Nano, ctx.Session.GetString(r.Context(), githubPendingStartedAtKey))
	age := time.Since(startedAt)
	if err != nil || age < -time.Minute || age > githubPendingIdentityTTL {
		return nil, errors.New("pending GitHub identity expired")
	}
	subject := ctx.Session.GetString(r.Context(), githubPendingSubjectKey)
	username := ctx.Session.GetString(r.Context(), githubPendingUsernameKey)
	csrf := ctx.Session.GetString(r.Context(), githubPendingCSRFKey)
	if subject == "" || username == "" || csrf == "" {
		return nil, errors.New("pending GitHub identity is incomplete")
	}
	return &githubPendingIdentity{
		Identity: &types.PersonOAuthIdentity{
			Provider: auth.OAuthProviderGitHub, Subject: subject, Username: username,
			Email:         ctx.Session.GetString(r.Context(), githubPendingEmailKey),
			EmailVerified: ctx.Session.GetBool(r.Context(), githubPendingEmailVerifiedKey),
			AvatarURL:     ctx.Session.GetString(r.Context(), githubPendingAvatarKey),
		},
		StartedAt: startedAt,
		Next:      auth.SafeNext(ctx.Session.GetString(r.Context(), githubPendingNextKey), "/dashboard"),
		CSRF:      csrf,
	}, nil
}

// requireGitHubLinkPerson preserves the pending OAuth proof while a new
// email-link user creates their person record. Existing users proceed directly
// to confirmation; users without even a verified email return to login.
func requireGitHubLinkPerson(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) *auth.Identity {
	viewer, err := auth.Resolve(r, ctx)
	if err != nil {
		ctx.Err.Printf("%s resolve person: %s", r.URL.Path, err)
		http.Error(w, "Unable to resolve account", http.StatusInternalServerError)
		return nil
	}
	if viewer != nil && viewer.PersonID != "" && viewer.Speaker != nil {
		return viewer
	}

	email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
	if email == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape("/auth/oauth/github/confirm"), http.StatusSeeOther)
		return nil
	}
	encodedEmail := base64.RawURLEncoding.EncodeToString([]byte(email))
	encodedHMAC := base64.RawURLEncoding.EncodeToString([]byte(helpers.CreateEmailHMAC(ctx, email)))
	destination := dashboardSpeakerEditURL(encodedHMAC, encodedEmail, "/auth/oauth/github/confirm")
	http.Redirect(w, r, destination, http.StatusSeeOther)
	return nil
}

func clearGitHubOAuthSession(ctx *config.AppContext, r *http.Request) {
	for _, key := range []string{githubOAuthStateKey, githubOAuthVerifierKey, githubOAuthStartedAtKey, githubOAuthModeKey, githubOAuthNextKey} {
		ctx.Session.Remove(r.Context(), key)
	}
}

func clearPendingGitHubIdentity(ctx *config.AppContext, r *http.Request) {
	for _, key := range []string{
		githubPendingSubjectKey, githubPendingUsernameKey, githubPendingEmailKey,
		githubPendingEmailVerifiedKey, githubPendingAvatarKey, githubPendingStartedAtKey,
		githubPendingNextKey, githubPendingCSRFKey,
	} {
		ctx.Session.Remove(r.Context(), key)
	}
}

func randomAuthToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate authentication token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func secureTokenEqual(expected, actual string) bool {
	if expected == "" || actual == "" || len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func redirectGitHubOAuthError(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, message string) {
	destination := "/login?error=" + url.QueryEscape(message)
	if auth.RequireOptional(r, ctx) != nil {
		destination = "/dashboard/emails?error=" + url.QueryEscape(message)
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func recordAuthAudit(ctx *config.AppContext, r *http.Request, personID, method, event string, metadata map[string]any) {
	err := getters.RecordAuthAuditEvent(ctx, &types.AuthAuditEvent{
		PersonID: personID, Method: method, Event: event,
		RemoteAddress: r.RemoteAddr, UserAgent: r.UserAgent(), Metadata: metadata,
	})
	if err != nil {
		ctx.Err.Printf("record auth audit %s: %s", event, err)
	}
}
