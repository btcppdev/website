package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
)

const passwordResetTTL = 30 * time.Minute

type PasswordResetPage struct {
	Token        string
	TokenValid   bool
	FlashMessage string
	FlashError   string
	CSRF         string
	Year         uint
}

func PasswordLogin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		redirectPasswordLogin(w, r, auth.SafeNext(r.FormValue("Next"), "/dashboard"), "That sign-in request expired. Reload the page and try again.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("Email")))
	if !allowAuthAttempt(ctx, r, email, 30, time.Minute) {
		http.Error(w, "Too many sign-in attempts. Try again in a minute.", http.StatusTooManyRequests)
		return
	}
	password := r.FormValue("Password")
	next := auth.SafeNext(r.FormValue("Next"), "/dashboard")
	credentialHash := auth.DummyPasswordHash()
	personID := ""
	var lockedUntil *time.Time
	resolution, err := getters.ResolvePersonByEmail(ctx, email)
	if err == nil && !resolution.IsConflict() && resolution.Alias != nil {
		personID = resolution.Alias.PersonID
		credential, credentialErr := getters.GetPersonPasswordCredential(ctx, personID)
		if credentialErr != nil {
			ctx.Err.Printf("password login credential lookup: %s", credentialErr)
		} else if credential != nil {
			credentialHash = credential.PasswordHash
			lockedUntil = credential.LockedUntil
		}
	}
	passwordWithinLimit := len([]rune(password)) <= auth.MaxPasswordLength
	if !passwordWithinLimit {
		password = "invalid-password"
	}
	valid := passwordWithinLimit && auth.CheckPassword(credentialHash, password)
	locked := lockedUntil != nil && lockedUntil.After(time.Now())
	if personID == "" || !valid || locked {
		if personID != "" && !valid {
			_ = getters.RecordPasswordFailure(ctx, personID)
		}
		recordAuthAudit(ctx, r, personID, string(auth.MethodPassword), "login_failed", nil)
		redirectPasswordLogin(w, r, next, "The email or password was incorrect.")
		return
	}
	if err := getters.RecordPasswordSuccess(ctx, personID); err != nil {
		ctx.Err.Printf("password login reset failures: %s", err)
	}
	if err := auth.LoginPerson(ctx, r, personID, auth.MethodPassword); err != nil {
		ctx.Err.Printf("password login session: %s", err)
		redirectPasswordLogin(w, r, next, "Unable to start your session. Try again.")
		return
	}
	recordAuthAudit(ctx, r, personID, string(auth.MethodPassword), "login_succeeded", nil)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func allowAuthAttempt(ctx *config.AppContext, r *http.Request, subject string, maximum int, window time.Duration) bool {
	peer := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	if peer == "" {
		peer = "unknown"
	}
	remote := peer
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		remote = forwarded
		if comma := strings.Index(remote, ","); comma >= 0 {
			remote = strings.TrimSpace(remote[:comma])
		}
	}
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if remote == "" {
		remote = "unknown"
	}
	type rateLimitKey struct {
		value   string
		maximum int
	}
	keys := []rateLimitKey{{value: "client-ip\x00" + remote, maximum: maximum}}
	// X-Forwarded-For is useful behind the production proxy, but the direct
	// peer gets a broader aggregate ceiling so spoofed forwarding headers
	// cannot remove throttling entirely.
	if peer != remote {
		keys = append(keys, rateLimitKey{value: "peer-ip\x00" + peer, maximum: maximum * 100})
	}
	if subject = strings.ToLower(strings.TrimSpace(subject)); subject != "" {
		keys = append(keys, rateLimitKey{value: "subject\x00" + subject, maximum: maximum})
	}
	for _, key := range keys {
		mac := hmac.New(sha256.New, ctx.Env.HMACKey[:])
		_, _ = mac.Write([]byte("auth-rate-limit-v1\x00" + r.URL.Path + "\x00" + key.value))
		allowed, err := getters.ConsumeAuthRateLimit(ctx, mac.Sum(nil), key.maximum, window)
		if err != nil {
			ctx.Err.Printf("auth rate limit %s: %s", r.URL.Path, err)
			return false
		}
		if !allowed {
			return false
		}
	}
	return true
}

func DashboardPasswordSet(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	viewer := requirePersonIdentity(w, r, ctx)
	if viewer == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		redirectPersonEmails(w, r, "", "That password request expired. Reload the page and try again.")
		return
	}
	if viewer.AuthenticatedAt.IsZero() || time.Since(viewer.AuthenticatedAt) > 15*time.Minute {
		redirectPersonEmails(w, r, "", "Sign in again before setting a password.")
		return
	}
	password := r.FormValue("password")
	if password != r.FormValue("password_confirm") {
		redirectPersonEmails(w, r, "", "The new passwords did not match.")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		redirectPersonEmails(w, r, "", err.Error())
		return
	}
	version, replaced, err := getters.SetPersonPassword(ctx, viewer.PersonID, hash)
	if err != nil {
		ctx.Err.Printf("set password for %s: %s", viewer.PersonID, err)
		redirectPersonEmails(w, r, "", "Unable to save that password.")
		return
	}
	if err := auth.RefreshSessionVersion(ctx, r, version); err != nil {
		ctx.Err.Printf("refresh session after password set: %s", err)
		auth.Logout(ctx, r)
		redirectPasswordLogin(w, r, "/dashboard/settings", "Password saved. Sign in again to continue.")
		return
	}
	auditEvent := "password_added"
	if replaced {
		auditEvent = "password_changed"
	}
	recordAuthAudit(ctx, r, viewer.PersonID, string(auth.MethodPassword), auditEvent, nil)
	sendPasswordChangedNotice(ctx, viewer.PersonID, identitySecurityEmail(viewer), replaced, time.Now().UTC())
	message := "Password added. Your other browser sessions remain signed in."
	if replaced {
		message = "Password saved. Other browser sessions were signed out."
	}
	redirectPersonEmails(w, r, message, "")
}

func ForgotPassword(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err == nil && secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
			email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
			if !allowAuthAttempt(ctx, r, email, 10, time.Minute) {
				http.Redirect(w, r, "/forgot-password?flash="+url.QueryEscape("If that email belongs to an account, a password reset link is on its way."), http.StatusSeeOther)
				return
			}
			resolution, resolveErr := getters.ResolvePersonByEmail(ctx, email)
			if resolveErr == nil && !resolution.IsConflict() && resolution.Alias != nil {
				token, tokenErr := getters.CreatePasswordResetToken(ctx, resolution.Alias.PersonID, email, passwordResetTTL)
				if tokenErr != nil {
					ctx.Err.Printf("create password reset token: %s", tokenErr)
				} else if err := sendPasswordResetEmail(ctx, email, token); err != nil {
					ctx.Err.Printf("send password reset email: %s", err)
				} else {
					recordAuthAudit(ctx, r, resolution.Alias.PersonID, string(auth.MethodPassword), "password_reset_requested", nil)
				}
			}
		}
		http.Redirect(w, r, "/forgot-password?flash="+url.QueryEscape("If that email belongs to an account, a password reset link is on its way."), http.StatusSeeOther)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to start password reset", http.StatusInternalServerError)
		return
	}
	renderPasswordResetPage(w, ctx, "forgot_password.tmpl", &PasswordResetPage{FlashMessage: r.URL.Query().Get("flash"), CSRF: csrf, Year: helpers.CurrentYear()})
}

func ResetPassword(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
			http.Redirect(w, r, "/reset-password?error="+url.QueryEscape("That reset request expired. Request another link."), http.StatusSeeOther)
			return
		}
		token = strings.TrimSpace(r.FormValue("token"))
		password := r.FormValue("password")
		if password != r.FormValue("password_confirm") {
			http.Redirect(w, r, "/reset-password?token="+url.QueryEscape(token)+"&error="+url.QueryEscape("The new passwords did not match."), http.StatusSeeOther)
			return
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			http.Redirect(w, r, "/reset-password?token="+url.QueryEscape(token)+"&error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		personID, _, err := getters.ConsumePasswordResetToken(ctx, token, hash)
		if errors.Is(err, getters.ErrPasswordResetTokenInvalid) {
			http.Redirect(w, r, "/reset-password?error="+url.QueryEscape("That reset link is invalid, expired, or already used."), http.StatusSeeOther)
			return
		}
		if err != nil {
			ctx.Err.Printf("consume password reset: %s", err)
			http.Error(w, "Unable to reset password", http.StatusInternalServerError)
			return
		}
		if err := auth.LoginPerson(ctx, r, personID, auth.MethodPassword); err != nil {
			ctx.Err.Printf("password reset login: %s", err)
			http.Redirect(w, r, "/login?flash="+url.QueryEscape("Password reset. Sign in with your new password."), http.StatusSeeOther)
			return
		}
		recordAuthAudit(ctx, r, personID, string(auth.MethodPassword), "password_reset_completed", nil)
		primary, _ := getters.GetPrimaryPersonEmail(ctx, personID)
		sendPasswordChangedNotice(ctx, personID, primary, true, time.Now().UTC())
		http.Redirect(w, r, "/dashboard?flash="+url.QueryEscape("Password reset. Other browser sessions were signed out."), http.StatusSeeOther)
		return
	}
	valid := false
	if token != "" {
		valid, _ = getters.PasswordResetTokenValid(ctx, token)
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to load password reset", http.StatusInternalServerError)
		return
	}
	renderPasswordResetPage(w, ctx, "reset_password.tmpl", &PasswordResetPage{Token: token, TokenValid: valid, FlashError: r.URL.Query().Get("error"), CSRF: csrf, Year: helpers.CurrentYear()})
}

func sendPasswordResetEmail(ctx *config.AppContext, email, token string) error {
	resetURL := strings.TrimRight(ctx.Env.GetURI(), "/") + "/reset-password?token=" + url.QueryEscape(token)
	if !ctx.InProduction {
		ctx.Infos.Printf("[dev] password reset link for %s: %s", email, resetURL)
	}
	markdown := fmt.Sprintf("# Reset your bitcoin++ password\n\n[Choose a new password](button#%s)\n\nThis link expires in 30 minutes and can only be used once. If you did not request it, you can ignore this email.\n\n— bitcoin++", resetURL)
	return sendPersonAccountEmail(ctx, email, "Reset your bitcoin++ password", markdown, "password-reset-"+token[:12])
}

func sendPasswordChangedNotice(ctx *config.AppContext, personID, email string, replaced bool, occurredAt time.Time) {
	if strings.TrimSpace(email) == "" {
		return
	}
	subject := "A password was added to your bitcoin++ account"
	detail := "A password was added as a new sign-in method. Your other browser sessions remain signed in."
	if replaced {
		subject = "Your bitcoin++ password changed"
		detail = "The password for your bitcoin++ account was changed. Other browser sessions were signed out."
	}
	sendAccountSecurityNotice(ctx, personID, email, subject, detail, occurredAt)
}

func renderPasswordResetPage(w http.ResponseWriter, ctx *config.AppContext, templateName string, page *PasswordResetPage) {
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, templateName, page); err != nil {
		ctx.Err.Printf("render %s: %s", templateName, err)
		http.Error(w, "Unable to render page", http.StatusInternalServerError)
	}
}

func redirectPasswordLogin(w http.ResponseWriter, r *http.Request, next, message string) {
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next)+"&error="+url.QueryEscape(message), http.StatusSeeOther)
}
