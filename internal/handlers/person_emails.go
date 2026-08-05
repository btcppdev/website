package handlers

import (
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
)

func DashboardPersonEmails(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requirePersonIdentity(w, r, ctx)
	if id == nil {
		return
	}
	addresses, err := getters.ListPersonEmails(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/emails list %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load email addresses", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_person_emails.tmpl", &PersonEmailsPage{
		Speaker:      id.Speaker,
		Emails:       addresses,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/dashboard/emails render: %s", err)
		http.Error(w, "Unable to render email addresses", http.StatusInternalServerError)
	}
}

func DashboardPersonEmailRequest(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requirePersonIdentity(w, r, ctx)
	if id == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectPersonEmails(w, r, "", "Invalid form submission.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	parsed, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(parsed.Address, email) {
		redirectPersonEmails(w, r, "", "Enter a valid email address.")
		return
	}
	token, err := getters.CreatePersonEmailVerification(ctx, id.PersonID, email, r.FormValue("make_primary") == "true")
	if err != nil {
		redirectPersonEmails(w, r, "", err.Error())
		return
	}
	verificationURL := strings.TrimRight(ctx.Env.GetURI(), "/") + "/dashboard/emails/verify?token=" + url.QueryEscape(token)
	if _, err := emails.OnlyForLoginLink(ctx, email, verificationURL); err != nil {
		ctx.Err.Printf("/dashboard/emails send verification %s: %s", email, err)
		redirectPersonEmails(w, r, "", "The verification email could not be sent. Try again.")
		return
	}
	redirectPersonEmails(w, r, "Check "+email+" for a verification link. It expires in 30 minutes.", "")
}

func DashboardPersonEmailVerify(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	personID, email, err := getters.ConsumePersonEmailVerification(ctx, r.URL.Query().Get("token"))
	if err != nil {
		redirectPersonEmails(w, r, "", err.Error())
		return
	}
	if err := auth.LoginPerson(ctx, r, personID, email); err != nil {
		ctx.Err.Printf("/dashboard/emails verified login %s: %s", personID, err)
		http.Error(w, "Email verified, but the session could not be updated. Sign in again.", http.StatusInternalServerError)
		return
	}
	redirectPersonEmails(w, r, email+" is now attached to your account.", "")
}

func DashboardPersonEmailPrimary(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requirePersonIdentity(w, r, ctx)
	if id == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectPersonEmails(w, r, "", "Invalid form submission.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if err := getters.SetPrimaryPersonEmail(ctx, id.PersonID, email); err != nil {
		redirectPersonEmails(w, r, "", err.Error())
		return
	}
	if err := auth.LoginPerson(ctx, r, id.PersonID, email); err != nil {
		ctx.Err.Printf("/dashboard/emails primary session %s: %s", id.PersonID, err)
	}
	redirectPersonEmails(w, r, email+" is now your primary email.", "")
}

func DashboardPersonEmailRemove(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requirePersonIdentity(w, r, ctx)
	if id == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectPersonEmails(w, r, "", "Invalid form submission.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if err := getters.RemovePersonEmail(ctx, id.PersonID, email); err != nil {
		redirectPersonEmails(w, r, "", err.Error())
		return
	}
	primary, err := getters.GetPrimaryPersonEmail(ctx, id.PersonID)
	if err == nil && primary != "" {
		if err := auth.LoginPerson(ctx, r, id.PersonID, primary); err != nil {
			ctx.Err.Printf("/dashboard/emails removal session %s: %s", id.PersonID, err)
		}
	}
	redirectPersonEmails(w, r, email+" was removed from your account.", "")
}

func requirePersonIdentity(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) *auth.Identity {
	id, err := auth.Resolve(r, ctx)
	if err != nil {
		ctx.Err.Printf("%s resolve person: %s", r.URL.Path, err)
	}
	if id == nil || id.PersonID == "" || id.Speaker == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return nil
	}
	return id
}

func redirectPersonEmails(w http.ResponseWriter, r *http.Request, flash, errorMessage string) {
	query := url.Values{}
	if flash != "" {
		query.Set("flash", flash)
	}
	if errorMessage != "" {
		query.Set("error", errorMessage)
	}
	destination := "/dashboard/emails"
	if encoded := query.Encode(); encoded != "" {
		destination += "?" + encoded
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}
