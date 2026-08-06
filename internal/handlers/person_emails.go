package handlers

import (
	"fmt"
	"net/http"
	"net/mail"
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
	mergeRequests, err := getters.ListPersonMergeRequestsForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/emails merge requests %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load merge requests", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_person_emails.tmpl", &PersonEmailsPage{
		Speaker:       id.Speaker,
		Emails:        addresses,
		MergeRequests: mergeRequests,
		FlashMessage:  r.URL.Query().Get("flash"),
		FlashError:    r.URL.Query().Get("error"),
		Year:          helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/dashboard/emails render: %s", err)
		http.Error(w, "Unable to render email addresses", http.StatusInternalServerError)
	}
}

func DashboardPersonMergeRequest(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requirePersonIdentity(w, r, ctx)
	if id == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		redirectPersonEmails(w, r, "", "Invalid form submission.")
		return
	}
	targetEmail := strings.ToLower(strings.TrimSpace(r.FormValue("target_email")))
	parsed, err := mail.ParseAddress(targetEmail)
	if err != nil || !strings.EqualFold(parsed.Address, targetEmail) {
		redirectPersonEmails(w, r, "", "Enter a valid email address for the other account.")
		return
	}
	request, token, err := getters.CreatePersonMergeRequest(ctx, id.PersonID, targetEmail)
	if err != nil {
		redirectPersonEmails(w, r, "", err.Error())
		return
	}
	if err := sendPersonMergeConfirmationEmail(ctx, request, token); err != nil {
		ctx.Err.Printf("person merge request %s confirmation email: %s", request.ID, err)
		redirectPersonEmails(w, r, "Your merge request was saved.", "The confirmation email could not be scheduled. Submit the request again to retry.")
		return
	}
	redirectPersonEmails(w, r, "We emailed "+targetEmail+". Administrators will be notified after that account confirms the request.", "")
}

func sendPersonMergeConfirmationEmail(ctx *config.AppContext, request *types.PersonMergeRequest, token string) error {
	if request == nil {
		return fmt.Errorf("merge request is required")
	}
	confirmationURL := strings.TrimRight(ctx.Env.GetURI(), "/") + "/account/merge/confirm?token=" + url.QueryEscape(token)
	markdown := fmt.Sprintf("# Confirm account merge\n\n%s (%s) asked to merge this bitcoin++ account into their account.\n\n[Merge accounts](button#%s)\n\nAfter you confirm, a global administrator will review both profiles before anything is changed. This confirmation link expires in 30 minutes. If you did not expect this request, you can ignore this email or contact hello@btcpp.dev.\n\n— bitcoin++", markdownEmailText(request.RequesterName), markdownEmailText(request.RequesterEmail), confirmationURL)
	return sendPersonMergeRequestEmail(ctx, request.TargetEmail, "Confirm your bitcoin++ account merge", markdown, "person-merge-target-"+request.ID+"-"+token[:12])
}

func sendPersonMergeAdminNotifications(ctx *config.AppContext, request *types.PersonMergeRequest) error {
	if request == nil {
		return fmt.Errorf("merge request is required")
	}
	reviewURL := strings.TrimRight(ctx.Env.GetURI(), "/") + "/admin/people/merge?request=" + url.QueryEscape(request.ID)
	admins, err := getters.ListSpeakersWithAnyRole(ctx, []string{"global-admin"})
	if err != nil {
		return err
	}
	if len(admins) == 0 {
		return fmt.Errorf("no global administrators have an email address")
	}
	var failures []string
	seen := map[string]bool{}
	for _, admin := range admins {
		if admin == nil {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(admin.Email))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		markdown := fmt.Sprintf("# New account merge request\n\n%s (%s) requested that %s (%s) be merged into their account.\n\n[Review this merge request](%s)\n\nNo records have been changed. The existing merge preview will require field-by-field review before completion.", markdownEmailText(request.RequesterName), markdownEmailText(request.RequesterEmail), markdownEmailText(request.TargetName), markdownEmailText(request.TargetEmail), reviewURL)
		if err := sendPersonMergeRequestEmail(ctx, email, "New bitcoin++ account merge request", markdown, "person-merge-admin-"+request.ID+"-"+admin.ID); err != nil {
			failures = append(failures, email+": "+err.Error())
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("no global administrators have an email address")
	}
	if len(failures) > 0 {
		return fmt.Errorf("notify global administrators: %s", strings.Join(failures, "; "))
	}
	return nil
}

type PersonMergeConfirmationPage struct {
	Request   *types.PersonMergeRequest
	Token     string
	Confirmed bool
	Error     string
	Year      uint
}

func PersonMergeConfirmation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	request, err := getters.GetPersonMergeRequestByConfirmationToken(ctx, token)
	page := &PersonMergeConfirmationPage{Request: request, Token: token, Year: helpers.CurrentYear()}
	if err != nil {
		page.Error = err.Error()
	} else if request.Status == "pending" && request.ConfirmedAt != nil {
		page.Confirmed = true
	} else if request.Status != "awaiting_confirmation" {
		page.Error = "This account merge request is no longer available."
	} else if request.ConfirmationExpiresAt == nil || time.Now().After(*request.ConfirmationExpiresAt) {
		page.Error = "This confirmation link has expired. Ask the other account to submit the request again."
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "person_merge_confirmation.tmpl", page); err != nil {
		ctx.Err.Printf("account merge confirmation render: %s", err)
		http.Error(w, "Unable to render account merge confirmation", http.StatusInternalServerError)
	}
}

func PersonMergeConfirmationAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/account/merge/confirm?error="+url.QueryEscape("Invalid form submission."), http.StatusSeeOther)
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	request, newlyConfirmed, err := getters.ConfirmPersonMergeRequest(ctx, token)
	if err != nil {
		page := &PersonMergeConfirmationPage{Token: token, Error: err.Error(), Year: helpers.CurrentYear()}
		if renderErr := ctx.TemplateCache.ExecuteTemplate(w, "person_merge_confirmation.tmpl", page); renderErr != nil {
			http.Error(w, "Unable to confirm account merge", http.StatusInternalServerError)
		}
		return
	}
	if newlyConfirmed {
		if err := sendPersonMergeAdminNotifications(ctx, request); err != nil {
			ctx.Err.Printf("person merge request %s admin notifications: %s", request.ID, err)
		}
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "person_merge_confirmation.tmpl", &PersonMergeConfirmationPage{
		Request: request, Token: token, Confirmed: true, Year: helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("account merge confirmation success render: %s", err)
		http.Error(w, "Account merge confirmed", http.StatusInternalServerError)
	}
}

func sendPersonMergeRequestEmail(ctx *config.AppContext, recipient, subject, markdown, jobKey string) error {
	htmlBody, err := emails.BuildHTMLEmail(ctx, []byte(markdown))
	if err != nil {
		return err
	}
	return emails.ComposeAndSendMail(ctx, &emails.Mail{
		JobKey:   jobKey,
		Email:    recipient,
		Title:    subject,
		SendAt:   time.Now(),
		HTMLBody: htmlBody,
		TextBody: []byte(strings.NewReplacer("# ", "", "**", "", "[", "", "]", "", "(", "", ")", "").Replace(markdown)),
	})
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
