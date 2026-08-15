package handlers

import (
	"context"
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

	"github.com/gorilla/mux"
)

const (
	oauthFlowTTL         = 10 * time.Minute
	oauthConfirmationTTL = 30 * time.Minute
	authMethodsCSRFKey   = "auth_methods_csrf"
)

type pendingOAuthIdentity struct {
	Identity  *types.PersonOAuthIdentity
	StartedAt time.Time
	Next      string
	CSRF      string
}

type OAuthConfirmPage struct {
	Speaker      *types.Speaker
	Identity     *types.PersonOAuthIdentity
	ProviderKey  string
	ProviderName string
	CSRF         string
	Year         uint
}

func OAuthStart(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	provider := requestOAuthProvider(r, ctx)
	if provider == nil {
		http.NotFound(w, r)
		return
	}
	if !provider.Enabled() {
		redirectOAuthError(w, r, ctx, provider.Label()+" sign-in is not configured yet.")
		return
	}

	state, err := randomAuthToken()
	if err != nil {
		ctx.Err.Printf("%s OAuth state: %s", provider.Label(), err)
		redirectOAuthError(w, r, ctx, "Unable to start "+provider.Label()+" sign-in. Try again.")
		return
	}
	verifier := ""
	challenge := ""
	if provider.UsesPKCE() {
		verifier, err = randomAuthToken()
		if err != nil {
			ctx.Err.Printf("%s OAuth verifier: %s", provider.Label(), err)
			redirectOAuthError(w, r, ctx, "Unable to start "+provider.Label()+" sign-in. Try again.")
			return
		}
		challengeBytes := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	}

	mode := "login"
	if auth.RequireOptional(r, ctx) != nil {
		mode = "link"
	}
	next := auth.SafeNext(r.URL.Query().Get("next"), "/dashboard")
	clearOAuthFlow(ctx, r, provider.Key())
	clearPendingOAuthIdentity(ctx, r, provider.Key())
	ctx.Session.Put(r.Context(), oauthKey(provider.Key(), "state"), state)
	ctx.Session.Put(r.Context(), oauthKey(provider.Key(), "verifier"), verifier)
	ctx.Session.Put(r.Context(), oauthKey(provider.Key(), "started_at"), time.Now().UTC().Format(time.RFC3339Nano))
	ctx.Session.Put(r.Context(), oauthKey(provider.Key(), "mode"), mode)
	ctx.Session.Put(r.Context(), oauthKey(provider.Key(), "next"), next)

	authorizationURL, err := provider.AuthorizationURL(state, challenge)
	if err != nil {
		ctx.Err.Printf("%s OAuth authorization URL: %s", provider.Label(), err)
		clearOAuthFlow(ctx, r, provider.Key())
		redirectOAuthError(w, r, ctx, "Unable to start "+provider.Label()+" sign-in. Try again.")
		return
	}
	http.Redirect(w, r, authorizationURL, http.StatusFound)
}

func OAuthCallback(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	provider := requestOAuthProvider(r, ctx)
	if provider == nil {
		http.NotFound(w, r)
		return
	}
	if !provider.Enabled() {
		redirectOAuthError(w, r, ctx, provider.Label()+" sign-in is not configured yet.")
		return
	}

	expectedState := ctx.Session.GetString(r.Context(), oauthKey(provider.Key(), "state"))
	verifier := ctx.Session.GetString(r.Context(), oauthKey(provider.Key(), "verifier"))
	startedAtRaw := ctx.Session.GetString(r.Context(), oauthKey(provider.Key(), "started_at"))
	mode := ctx.Session.GetString(r.Context(), oauthKey(provider.Key(), "mode"))
	next := auth.SafeNext(ctx.Session.GetString(r.Context(), oauthKey(provider.Key(), "next")), "/dashboard")
	clearOAuthFlow(ctx, r, provider.Key())

	startedAt, startedAtErr := time.Parse(time.RFC3339Nano, startedAtRaw)
	age := time.Since(startedAt)
	missingVerifier := provider.UsesPKCE() && verifier == ""
	if expectedState == "" || missingVerifier || startedAtErr != nil || age < -time.Minute || age > oauthFlowTTL ||
		!secureTokenEqual(expectedState, r.URL.Query().Get("state")) {
		redirectOAuthError(w, r, ctx, "That "+provider.Label()+" sign-in attempt expired or was already used. Try again.")
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("error")) != "" {
		redirectOAuthError(w, r, ctx, provider.Label()+" sign-in was cancelled.")
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		redirectOAuthError(w, r, ctx, provider.Label()+" did not return a sign-in code. Try again.")
		return
	}

	providerContext, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	token, err := provider.Exchange(providerContext, code, verifier)
	if err != nil {
		ctx.Err.Printf("%s OAuth exchange: %s", provider.Label(), err)
		redirectOAuthError(w, r, ctx, provider.Label()+" sign-in could not be verified. Try again.")
		return
	}
	providerIdentity, err := provider.FetchIdentity(providerContext, token)
	if err != nil {
		ctx.Err.Printf("%s OAuth identity: %s", provider.Label(), err)
		redirectOAuthError(w, r, ctx, provider.Label()+" account details could not be loaded. Try again.")
		return
	}

	linked, err := getters.FindOAuthIdentity(ctx, provider.Key(), providerIdentity.Subject)
	if err != nil {
		ctx.Err.Printf("%s OAuth identity lookup: %s", provider.Label(), err)
		redirectOAuthError(w, r, ctx, provider.Label()+" sign-in could not be completed. Try again.")
		return
	}
	viewer := auth.RequireOptional(r, ctx)
	if linked != nil {
		if viewer != nil && viewer.PersonID != linked.PersonID {
			recordAuthAudit(ctx, r, viewer.PersonID, provider.Key(), "oauth_link_conflict", map[string]any{"provider_subject": providerIdentity.Subject})
			redirectOAuthError(w, r, ctx, "That "+provider.Label()+" account is already linked to another bitcoin++ profile.")
			return
		}
		providerIdentity, err = getters.LinkOAuthIdentity(ctx, linked.PersonID, providerIdentity)
		if err != nil {
			ctx.Err.Printf("%s OAuth refresh identity: %s", provider.Label(), err)
			redirectOAuthError(w, r, ctx, provider.Label()+" sign-in could not be completed. Try again.")
			return
		}
		if err := getters.MarkOAuthIdentityLogin(ctx, providerIdentity.ID); err != nil {
			ctx.Err.Printf("%s OAuth last login: %s", provider.Label(), err)
		}
		if err := auth.LoginPerson(ctx, r, linked.PersonID, auth.Method(provider.Key())); err != nil {
			ctx.Err.Printf("%s OAuth session: %s", provider.Label(), err)
			redirectOAuthError(w, r, ctx, provider.Label()+" was verified, but the session could not be started. Try again.")
			return
		}
		recordAuthAudit(ctx, r, linked.PersonID, provider.Key(), "login_succeeded", map[string]any{"oauth_identity_id": providerIdentity.ID})
		if mode == "link" {
			next = "/dashboard/emails?flash=" + url.QueryEscape(oauthIdentityLabel(provider.Label(), providerIdentity.Username)+" is already linked.")
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}

	if err := storePendingOAuthIdentity(ctx, r, provider.Key(), providerIdentity, next); err != nil {
		ctx.Err.Printf("%s pending OAuth identity: %s", provider.Label(), err)
		redirectOAuthError(w, r, ctx, provider.Label()+" sign-in could not be completed. Try again.")
		return
	}
	confirmationPath := "/auth/oauth/" + provider.Key() + "/confirm"
	if viewer == nil {
		destination := "/login?next=" + url.QueryEscape(confirmationPath) +
			"&flash=" + url.QueryEscape(oauthIdentityLabel(provider.Label(), providerIdentity.Username)+" was verified. Sign in by email once to choose the bitcoin++ profile to link.")
		http.Redirect(w, r, destination, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, confirmationPath, http.StatusSeeOther)
}

func OAuthConfirm(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	provider := requestOAuthProvider(r, ctx)
	if provider == nil {
		http.NotFound(w, r)
		return
	}
	viewer := requireOAuthLinkPerson(w, r, ctx, provider.Key())
	if viewer == nil {
		return
	}
	pending, err := pendingOAuth(ctx, r, provider.Key())
	if err != nil {
		clearPendingOAuthIdentity(ctx, r, provider.Key())
		redirectPersonEmails(w, r, "", "That "+provider.Label()+" link attempt expired. Start again.")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "oauth_confirm.tmpl", &OAuthConfirmPage{
		Speaker: viewer.Speaker, Identity: pending.Identity, ProviderKey: provider.Key(), ProviderName: provider.Label(), CSRF: pending.CSRF, Year: helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("%s OAuth confirmation render: %s", provider.Label(), err)
		http.Error(w, "Unable to render account confirmation", http.StatusInternalServerError)
	}
}

func OAuthConfirmAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	provider := requestOAuthProvider(r, ctx)
	if provider == nil {
		http.NotFound(w, r)
		return
	}
	viewer := requireOAuthLinkPerson(w, r, ctx, provider.Key())
	if viewer == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		redirectPersonEmails(w, r, "", "Invalid "+provider.Label()+" confirmation.")
		return
	}
	pending, err := pendingOAuth(ctx, r, provider.Key())
	if err != nil || !secureTokenEqual(pending.CSRF, r.FormValue("csrf")) {
		clearPendingOAuthIdentity(ctx, r, provider.Key())
		recordAuthAudit(ctx, r, viewer.PersonID, provider.Key(), "oauth_link_confirmation_rejected", nil)
		redirectPersonEmails(w, r, "", "That "+provider.Label()+" confirmation expired or was already used. Start again.")
		return
	}
	clearPendingOAuthIdentity(ctx, r, provider.Key())

	linked, err := getters.LinkOAuthIdentity(ctx, viewer.PersonID, pending.Identity)
	if errors.Is(err, getters.ErrOAuthIdentityLinked) {
		recordAuthAudit(ctx, r, viewer.PersonID, provider.Key(), "oauth_link_conflict", map[string]any{"provider_subject": pending.Identity.Subject})
		redirectPersonEmails(w, r, "", "That "+provider.Label()+" account is already linked to another bitcoin++ profile.")
		return
	}
	if err != nil {
		ctx.Err.Printf("%s OAuth link: %s", provider.Label(), err)
		redirectPersonEmails(w, r, "", provider.Label()+" could not be linked. Try again.")
		return
	}
	if err := getters.MarkOAuthIdentityLogin(ctx, linked.ID); err != nil {
		ctx.Err.Printf("%s OAuth linked login time: %s", provider.Label(), err)
	}
	if err := auth.LoginPerson(ctx, r, viewer.PersonID, auth.Method(provider.Key())); err != nil {
		ctx.Err.Printf("%s OAuth linked session: %s", provider.Label(), err)
		redirectPersonEmails(w, r, "", provider.Label()+" was linked, but the session could not be refreshed.")
		return
	}
	recordAuthAudit(ctx, r, viewer.PersonID, provider.Key(), "oauth_identity_linked", map[string]any{"oauth_identity_id": linked.ID})
	destination := auth.SafeNext(pending.Next, "/dashboard/emails")
	if destination == "/dashboard" {
		destination = "/dashboard/emails"
	}
	http.Redirect(w, r, appendFlash(destination, oauthIdentityLabel(provider.Label(), linked.Username)+" is now linked."), http.StatusSeeOther)
}

func OAuthUnlink(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	provider := requestOAuthProvider(r, ctx)
	if provider == nil {
		http.NotFound(w, r)
		return
	}
	viewer := requirePersonIdentity(w, r, ctx)
	if viewer == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		recordAuthAudit(ctx, r, viewer.PersonID, provider.Key(), "oauth_unlink_rejected", nil)
		redirectPersonEmails(w, r, "", "That unlink request expired. Reload the page and try again.")
		return
	}
	identity, err := getters.UnlinkOAuthIdentity(ctx, viewer.PersonID, provider.Key(), strings.TrimSpace(r.FormValue("identity_id")))
	if err != nil {
		ctx.Err.Printf("%s OAuth unlink: %s", provider.Label(), err)
		redirectPersonEmails(w, r, "", provider.Label()+" could not be unlinked. Try again.")
		return
	}
	if identity == nil {
		redirectPersonEmails(w, r, "", "That "+provider.Label()+" identity is no longer linked.")
		return
	}
	recordAuthAudit(ctx, r, viewer.PersonID, provider.Key(), "oauth_identity_unlinked", map[string]any{"provider_subject": identity.Subject})
	redirectPersonEmails(w, r, oauthIdentityLabel(provider.Label(), identity.Username)+" was unlinked.", "")
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

func storePendingOAuthIdentity(ctx *config.AppContext, r *http.Request, provider string, identity *types.PersonOAuthIdentity, next string) error {
	csrf, err := randomAuthToken()
	if err != nil {
		return err
	}
	clearPendingOAuthIdentity(ctx, r, provider)
	ctx.Session.Put(r.Context(), oauthPendingKey(provider, "subject"), identity.Subject)
	ctx.Session.Put(r.Context(), oauthPendingKey(provider, "username"), identity.Username)
	ctx.Session.Put(r.Context(), oauthPendingKey(provider, "email"), identity.Email)
	ctx.Session.Put(r.Context(), oauthPendingKey(provider, "email_verified"), identity.EmailVerified)
	ctx.Session.Put(r.Context(), oauthPendingKey(provider, "avatar"), identity.AvatarURL)
	ctx.Session.Put(r.Context(), oauthPendingKey(provider, "started_at"), time.Now().UTC().Format(time.RFC3339Nano))
	ctx.Session.Put(r.Context(), oauthPendingKey(provider, "next"), auth.SafeNext(next, "/dashboard"))
	ctx.Session.Put(r.Context(), oauthPendingKey(provider, "csrf"), csrf)
	return nil
}

func pendingOAuth(ctx *config.AppContext, r *http.Request, provider string) (*pendingOAuthIdentity, error) {
	startedAt, err := time.Parse(time.RFC3339Nano, ctx.Session.GetString(r.Context(), oauthPendingKey(provider, "started_at")))
	age := time.Since(startedAt)
	if err != nil || age < -time.Minute || age > oauthConfirmationTTL {
		return nil, errors.New("pending OAuth identity expired")
	}
	subject := ctx.Session.GetString(r.Context(), oauthPendingKey(provider, "subject"))
	username := ctx.Session.GetString(r.Context(), oauthPendingKey(provider, "username"))
	csrf := ctx.Session.GetString(r.Context(), oauthPendingKey(provider, "csrf"))
	if subject == "" || username == "" || csrf == "" {
		return nil, errors.New("pending OAuth identity is incomplete")
	}
	return &pendingOAuthIdentity{
		Identity: &types.PersonOAuthIdentity{
			Provider: provider, Subject: subject, Username: username,
			Email:         ctx.Session.GetString(r.Context(), oauthPendingKey(provider, "email")),
			EmailVerified: ctx.Session.GetBool(r.Context(), oauthPendingKey(provider, "email_verified")),
			AvatarURL:     ctx.Session.GetString(r.Context(), oauthPendingKey(provider, "avatar")),
		},
		StartedAt: startedAt,
		Next:      auth.SafeNext(ctx.Session.GetString(r.Context(), oauthPendingKey(provider, "next")), "/dashboard"),
		CSRF:      csrf,
	}, nil
}

func requireOAuthLinkPerson(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, provider string) *auth.Identity {
	viewer, err := auth.Resolve(r, ctx)
	if err != nil {
		ctx.Err.Printf("%s resolve person: %s", r.URL.Path, err)
		http.Error(w, "Unable to resolve account", http.StatusInternalServerError)
		return nil
	}
	if viewer != nil && viewer.PersonID != "" && viewer.Speaker != nil {
		return viewer
	}
	confirmationPath := "/auth/oauth/" + provider + "/confirm"
	email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
	if email == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(confirmationPath), http.StatusSeeOther)
		return nil
	}
	encodedEmail := base64.RawURLEncoding.EncodeToString([]byte(email))
	encodedHMAC := base64.RawURLEncoding.EncodeToString([]byte(helpers.CreateEmailHMAC(ctx, email)))
	http.Redirect(w, r, dashboardSpeakerEditURL(encodedHMAC, encodedEmail, confirmationPath), http.StatusSeeOther)
	return nil
}

func requestOAuthProvider(r *http.Request, ctx *config.AppContext) auth.OAuthProvider {
	return auth.OAuthProviderByKey(ctx.Env, mux.Vars(r)["provider"])
}

func oauthKey(provider, suffix string) string { return "oauth_" + provider + "_" + suffix }
func oauthPendingKey(provider, suffix string) string {
	return "oauth_" + provider + "_pending_" + suffix
}

func clearOAuthFlow(ctx *config.AppContext, r *http.Request, provider string) {
	for _, suffix := range []string{"state", "verifier", "started_at", "mode", "next"} {
		ctx.Session.Remove(r.Context(), oauthKey(provider, suffix))
	}
}

func clearPendingOAuthIdentity(ctx *config.AppContext, r *http.Request, provider string) {
	for _, suffix := range []string{"subject", "username", "email", "email_verified", "avatar", "started_at", "next", "csrf"} {
		ctx.Session.Remove(r.Context(), oauthPendingKey(provider, suffix))
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

func redirectOAuthError(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, message string) {
	destination := "/login?error=" + url.QueryEscape(message)
	if auth.RequireOptional(r, ctx) != nil {
		destination = "/dashboard/emails?error=" + url.QueryEscape(message)
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func oauthIdentityLabel(provider, username string) string {
	if strings.TrimSpace(username) == "" {
		return provider
	}
	if provider == "Major League Hacking" {
		return provider + " account for " + username
	}
	return provider + " @" + username
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
