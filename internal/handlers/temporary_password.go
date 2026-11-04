package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

const temporaryPersonKey = auth.TemporaryPasswordPersonKey
const temporaryHashKey = auth.TemporaryPasswordProofKey
const temporaryUntilKey = auth.TemporaryPasswordUntilKey
const temporaryNextKey = auth.TemporaryPasswordNextKey

type TemporaryPasswordPage struct {
	CSRF, Email, Password, Error, Message string
	Admin                                 bool
	Year                                  uint
}

func renderTemporaryPassword(w http.ResponseWriter, ctx *config.AppContext, page TemporaryPasswordPage) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	page.Year = helpers.CurrentYear()
	if err := ctx.TemplateCache.ExecuteTemplate(w, "temporary_password.tmpl", page); err != nil {
		ctx.Err.Printf("temporary password template: %s", err)
	}
}

func AdminTemporaryPassword(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	admin := requireGlobalAdmin(w, r, ctx)
	if admin == nil {
		return
	}
	serveAdminTemporaryPassword(w, r, ctx, admin)
}

func serveAdminTemporaryPassword(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, admin *auth.Identity) {
	if admin == nil || !admin.IsGlobalAdmin() {
		http.Error(w, "Global administrator required", 403)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to secure form", 500)
		return
	}
	page := TemporaryPasswordPage{Admin: true, CSRF: csrf}
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil || !secureTokenEqual(csrf, r.FormValue("csrf")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		if admin.AuthenticatedAt.IsZero() || time.Since(admin.AuthenticatedAt) > 15*time.Minute {
			page.Error = "Sign in again before issuing a temporary password."
			renderTemporaryPassword(w, ctx, page)
			return
		}
		page.Email = strings.ToLower(strings.TrimSpace(r.FormValue("email")))
		resolved, err := getters.ResolvePersonByEmail(ctx, page.Email)
		if err != nil || resolved.IsConflict() || resolved.Alias == nil {
			page.Error = "No unambiguous account found for that email."
			renderTemporaryPassword(w, ctx, page)
			return
		}
		if resolved.Alias.PersonID == admin.PersonID {
			page.Error = "Use your account settings to change your own password."
			renderTemporaryPassword(w, ctx, page)
			return
		}
		password := r.FormValue("password")
		if password == "" {
			raw := make([]byte, 18)
			if _, err := rand.Read(raw); err != nil {
				http.Error(w, "Unable to generate password", 500)
				return
			}
			password = base64.RawURLEncoding.EncodeToString(raw)
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			page.Error = err.Error()
			renderTemporaryPassword(w, ctx, page)
			return
		}
		if err := getters.IssueTemporaryPassword(ctx, resolved.Alias.PersonID, hash); err != nil {
			ctx.Err.Printf("issue temporary password: %s", err)
			page.Error = "Unable to issue temporary password."
			renderTemporaryPassword(w, ctx, page)
			return
		}
		recordAuthAudit(ctx, r, resolved.Alias.PersonID, string(auth.MethodPassword), "temporary_password_issued", map[string]interface{}{"admin_person_id": admin.PersonID})
		page.Password = password
		page.Message = "Temporary password issued. Share it directly with this person. It expires in 72 hours; they must replace it before accessing their account. Previous sessions and password-reset links have been revoked."
	}
	renderTemporaryPassword(w, ctx, page)
}

func beginTemporaryPasswordChange(ctx *config.AppContext, r *http.Request, person, hash, next string) error {
	// This is deliberately NOT an authenticated person session. No dashboard,
	// admin, OAuth authorization, or account APIs can use this proof.
	if err := ctx.Session.Destroy(r.Context()); err != nil {
		return err
	}
	if err := ctx.Session.RenewToken(r.Context()); err != nil {
		return err
	}
	ctx.Session.Put(r.Context(), temporaryPersonKey, person)
	ctx.Session.Put(r.Context(), temporaryHashKey, hash)
	ctx.Session.Put(r.Context(), temporaryUntilKey, time.Now().Add(15*time.Minute))
	ctx.Session.Put(r.Context(), temporaryNextKey, auth.SafeNext(next, "/dashboard"))
	return nil
}

func ChangeTemporaryPassword(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	person := ctx.Session.GetString(r.Context(), temporaryPersonKey)
	proof := ctx.Session.GetString(r.Context(), temporaryHashKey)
	if person == "" || proof == "" || !ctx.Session.GetTime(r.Context(), temporaryUntilKey).After(time.Now()) {
		auth.ClearTemporaryPasswordProof(ctx, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to secure form", 500)
		return
	}
	page := TemporaryPasswordPage{CSRF: csrf}
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil || !secureTokenEqual(csrf, r.FormValue("csrf")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		password := r.FormValue("password")
		if err := auth.ValidatePassword(password); err != nil {
			page.Error = err.Error()
		} else if password != r.FormValue("password_confirm") {
			page.Error = "The passwords did not match."
		} else if auth.CheckPassword(proof, password) {
			page.Error = "Choose a different password from your temporary password."
		}
		if page.Error != "" {
			renderTemporaryPassword(w, ctx, page)
			return
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			http.Error(w, "Unable to save password", 500)
			return
		}
		if err := getters.ReplaceTemporaryPassword(ctx, person, proof, hash); err != nil {
			page.Error = "This temporary sign-in expired or was replaced. Sign in again."
			renderTemporaryPassword(w, ctx, page)
			return
		}
		next := auth.SafeNext(ctx.Session.GetString(r.Context(), temporaryNextKey), "/dashboard")
		ctx.Session.Remove(r.Context(), temporaryPersonKey)
		ctx.Session.Remove(r.Context(), temporaryHashKey)
		ctx.Session.Remove(r.Context(), temporaryUntilKey)
		ctx.Session.Remove(r.Context(), temporaryNextKey)
		if err := auth.LoginPerson(ctx, r, person, auth.MethodPassword); err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		recordAuthAudit(ctx, r, person, string(auth.MethodPassword), "temporary_password_replaced", nil)
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	renderTemporaryPassword(w, ctx, page)
}
