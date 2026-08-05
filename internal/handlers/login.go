package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
)

// LoginPage drives /login — an email-entry form that sends a
// magic-link landing on /auth and then bouncing to a sanitized
// `next` path. Used by every page guarded by auth.RequireRole when
// the user isn't authenticated yet.
type LoginPage struct {
	Next         string
	FlashMessage string
	FlashError   string
	Year         uint
}

const (
	sessionAccountPhotoPersonKey = "account_photo_person_id"
	sessionAccountPhotoURLKey    = "account_photo_url"
)

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
		email := strings.TrimSpace(r.PostForm.Get("Email"))
		next := auth.SafeNext(r.PostForm.Get("Next"), "/dashboard")
		if email == "" {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(next)+"&flash="+url.QueryEscape("Enter the email you want a login link sent to."), http.StatusSeeOther)
			return
		}
		link := auth.MagicLink(ctx, email, next)
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

	page := &LoginPage{
		Next:         auth.SafeNext(r.URL.Query().Get("next"), "/dashboard"),
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
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

// AuthStatus lets shared chrome adjust account-menu actions without
// threading request/session state through every template page model.
func AuthStatus(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
	personID := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionPersonIDKey))
	photoURL := accountPhotoURL(r, ctx, personID)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Authenticated bool   `json:"authenticated"`
		PhotoURL      string `json:"photoUrl,omitempty"`
	}{
		Authenticated: email != "",
		PhotoURL:      photoURL,
	})
}

func accountPhotoURL(r *http.Request, ctx *config.AppContext, personID string) string {
	if personID == "" {
		return ""
	}
	if ctx.Session.GetString(r.Context(), sessionAccountPhotoPersonKey) == personID {
		return ctx.Session.GetString(r.Context(), sessionAccountPhotoURLKey)
	}

	photoURL := ""
	speaker, err := getters.FetchSpeakerByID(ctx, personID)
	if err != nil {
		ctx.Err.Printf("/auth/status person lookup %s: %s", personID, err)
		return ""
	}
	if speaker != nil && strings.TrimSpace(speaker.Photo) != "" {
		photoURL = SpeakerPhotoURL(ctx, speaker.Photo)
	}
	ctx.Session.Put(r.Context(), sessionAccountPhotoPersonKey, personID)
	ctx.Session.Put(r.Context(), sessionAccountPhotoURLKey, photoURL)
	return photoURL
}

func invalidateAccountPhotoSession(r *http.Request, ctx *config.AppContext) {
	ctx.Session.Remove(r.Context(), sessionAccountPhotoPersonKey)
	ctx.Session.Remove(r.Context(), sessionAccountPhotoURLKey)
}

// LogoutHandler clears the auth session and bounces home. POST so
// it isn't trivially CSRF'd via an <img src=...> trick.
func LogoutHandler(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	invalidateAccountPhotoSession(r, ctx)
	auth.Logout(ctx, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
