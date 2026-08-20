package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

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

// AuthLanding is the /auth handler that magic-links point at. Thin
// wrapper around auth.AuthRedirect so the route registration in
// handlers.go can keep the pattern of `func(w,r) { Foo(w,r,app) }`.
func AuthLanding(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	auth.AuthRedirect(w, r, ctx)
}

// LogoutHandler clears the auth session and bounces home. POST so
// it isn't trivially CSRF'd via an <img src=...> trick.
func LogoutHandler(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
