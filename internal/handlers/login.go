package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

// LoginPage drives /login — an email-entry form that sends a
// magic-link landing on /auth and then bouncing to a sanitized
// `next` path. Used by every page guarded by auth.RequireRole when
// the user isn't authenticated yet.
type LoginPage struct {
	Next            string
	FlashMessage    string
	FlashError      string
	OAuthProviders  []*OAuthProviderView
	DevLoginEnabled bool
	CSRF            string
	Year            uint
}

type MagicLinkPage struct {
	Token      string
	TokenValid bool
	CanResend  bool
	FlashError string
	CSRF       string
	Year       uint
}

// Login renders the email-entry form (GET) and dispatches the
// magic-link email (POST). On POST it always redirects back to /login
// with a flash, regardless of whether the email is on file — that
// keeps us from leaking whether an email is registered.
func Login(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		next := auth.SafeNext(r.PostForm.Get("Next"), "/dashboard")
		email := strings.TrimSpace(r.PostForm.Get("Email"))
		if r.PostForm.Get("Action") == "dev-login" {
			if !dashboardDevLoginEnabled(ctx) {
				http.NotFound(w, r)
				return
			}
			if !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(next)+"&error="+url.QueryEscape("That development sign-in request expired. Reload and try again."), http.StatusSeeOther)
				return
			}
			if email == "" {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(next)+"&error="+url.QueryEscape("Enter a seeded email for development sign-in."), http.StatusSeeOther)
				return
			}
			if err := auth.LoginEmail(ctx, r, email); err != nil {
				ctx.Err.Printf("/login development login failed: %s", err)
				http.Redirect(w, r, "/login?next="+url.QueryEscape(next)+"&error="+url.QueryEscape("Unable to sign in as that email."), http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
		if !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(next)+"&error="+url.QueryEscape("That email-link request expired. Reload and try again."), http.StatusSeeOther)
			return
		}
		if email == "" {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(next)+"&flash="+url.QueryEscape("Enter the email you want a login link sent to."), http.StatusSeeOther)
			return
		}
		if !allowAuthAttempt(ctx, r, strings.ToLower(email), 10, time.Minute) {
			http.Redirect(w, r, "/login?flash="+url.QueryEscape("Check your inbox — if that address can sign in, a login link is on its way."), http.StatusSeeOther)
			return
		}
		link := auth.MagicLink(ctx, email, next)
		if link == "" {
			ctx.Err.Printf("/login create magic link for %s", email)
			http.Error(w, "Couldn't create the login link — try again in a minute.", http.StatusInternalServerError)
			return
		}
		if _, err := emails.OnlyForLoginLink(ctx, email, link); err != nil {
			ctx.Err.Printf("/login send %s: %s", email, err)
			http.Error(w, "Couldn't send the email — try again in a minute.", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r,
			"/login?flash="+url.QueryEscape("Check your inbox — we sent you a login link."),
			http.StatusSeeOther)
		return
	}

	next := auth.SafeNext(r.URL.Query().Get("next"), "/dashboard")
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to start sign-in", http.StatusInternalServerError)
		return
	}
	providers := enabledOAuthProviderViews(ctx.Env, next)
	if dashboardDevLoginEnabled(ctx) {
		providers = oauthProviderViews(ctx.Env, next)
	}
	page := &LoginPage{
		Next: next, FlashMessage: r.URL.Query().Get("flash"), FlashError: r.URL.Query().Get("error"),
		OAuthProviders: providers, DevLoginEnabled: dashboardDevLoginEnabled(ctx), CSRF: csrf, Year: helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "login.tmpl", page); err != nil {
		ctx.Err.Printf("/login render: %s", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

// AuthLanding renders a non-consuming GET confirmation so email security
// scanners cannot spend a one-use credential. Only the CSRF-protected POST
// consumes the token and starts the session.
func AuthLanding(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
			http.Redirect(w, r, "/auth?token="+url.QueryEscape(strings.TrimSpace(r.FormValue("token")))+"&error="+url.QueryEscape("That confirmation expired. Reload the page and try again."), http.StatusSeeOther)
			return
		}
		auth.AuthRedirect(w, r, ctx)
		return
	}
	_, _, valid, found, err := getters.LookupMagicLoginToken(ctx, token)
	if err != nil {
		ctx.Err.Printf("/auth inspect magic login token: %s", err)
		http.Error(w, "Unable to check that login link", http.StatusInternalServerError)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to confirm sign-in", http.StatusInternalServerError)
		return
	}
	renderMagicLinkPage(w, ctx, &MagicLinkPage{
		Token: token, TokenValid: valid, CanResend: found,
		FlashError: r.URL.Query().Get("error"), CSRF: csrf, Year: helpers.CurrentYear(),
	})
}

func MagicLinkResend(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Redirect(w, r, "/login?error="+url.QueryEscape("That resend request expired. Request another login link."), http.StatusSeeOther)
		return
	}
	email, next, _, found, err := getters.LookupMagicLoginToken(ctx, strings.TrimSpace(r.FormValue("token")))
	if err != nil {
		ctx.Err.Printf("/auth/resend inspect magic login token: %s", err)
		http.Error(w, "Unable to resend that login link", http.StatusInternalServerError)
		return
	}
	genericMessage := "If that link belonged to an email address, a fresh login link is on its way."
	if !found || !allowAuthAttempt(ctx, r, strings.ToLower(email), 5, 15*time.Minute) {
		http.Redirect(w, r, "/login?flash="+url.QueryEscape(genericMessage), http.StatusSeeOther)
		return
	}
	link := auth.MagicLink(ctx, email, auth.SafeNext(next, "/dashboard"))
	if link == "" {
		ctx.Err.Printf("/auth/resend create magic link")
		http.Error(w, "Unable to resend that login link", http.StatusInternalServerError)
		return
	}
	if _, err := emails.OnlyForLoginLink(ctx, email, link); err != nil {
		ctx.Err.Printf("/auth/resend send login link: %s", err)
		http.Error(w, "Unable to resend that login link", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login?flash="+url.QueryEscape(genericMessage), http.StatusSeeOther)
}

func renderMagicLinkPage(w http.ResponseWriter, ctx *config.AppContext, page *MagicLinkPage) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "magic_login.tmpl", page); err != nil {
		ctx.Err.Printf("/auth render: %s", err)
		http.Error(w, "Unable to render login confirmation", http.StatusInternalServerError)
	}
}

// LogoutHandler clears the auth session and bounces home. The shared account
// menu supplies the same session-bound CSRF token used by other credential
// actions, preventing another site from silently signing the visitor out.
func LogoutHandler(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "That sign-out request expired. Reload and try again.", http.StatusForbidden)
		return
	}
	for _, provider := range auth.OAuthProviders(ctx.Env) {
		clearOAuthFlow(ctx, r, provider.Key())
		clearPendingOAuthIdentity(ctx, r, provider.Key())
	}
	clearNostrChallenge(ctx, r)
	ctx.Session.Remove(r.Context(), passkeyLoginSessionKey)
	ctx.Session.Remove(r.Context(), passkeyLoginNextKey)
	clearPasskeyRegistration(ctx, r)
	ctx.Session.Remove(r.Context(), authMethodsCSRFKey)
	auth.Logout(ctx, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func oauthProviderViews(env *types.EnvConfig, next string) []*OAuthProviderView {
	descriptions := map[string]string{
		"github":  "Uses your immutable GitHub account ID. Repository access is never requested.",
		"discord": "Uses your Discord account ID. Server and message access is never requested.",
		"gitlab":  "Uses your GitLab account ID. Repository access is never requested.",
		"mlh":     "Uses your Major League Hacking profile and primary email.",
	}
	providers := auth.OAuthProviders(env)
	views := make([]*OAuthProviderView, 0, len(providers))
	for _, provider := range providers {
		views = append(views, &OAuthProviderView{
			Key: provider.Key(), Label: provider.Label(), Enabled: provider.Enabled(),
			LinkURL:     "/auth/oauth/" + provider.Key() + "?next=" + url.QueryEscape(next),
			Description: descriptions[provider.Key()],
		})
	}
	return views
}

func enabledOAuthProviderViews(env *types.EnvConfig, next string) []*OAuthProviderView {
	var enabled []*OAuthProviderView
	for _, provider := range oauthProviderViews(env, next) {
		if provider.Enabled {
			enabled = append(enabled, provider)
		}
	}
	return enabled
}
