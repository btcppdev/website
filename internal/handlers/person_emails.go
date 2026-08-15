package handlers

import (
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"reflect"
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
	pendingEmails, err := getters.ListPendingPersonEmailVerifications(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/emails pending list %s: %s", id.PersonID, err)
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
		PendingEmails: pendingEmails,
		MergeRequests: mergeRequests,
		FlashMessage:  r.URL.Query().Get("flash"),
		FlashError:    r.URL.Query().Get("error"),
		Year:          helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/dashboard/emails render: %s", err)
		http.Error(w, "Unable to render email addresses", http.StatusInternalServerError)
	}
}

func sendPersonMergeConfirmationEmail(ctx *config.AppContext, request *types.PersonMergeRequest, token string) error {
	if request == nil {
		return fmt.Errorf("merge request is required")
	}
	confirmationURL := strings.TrimRight(ctx.Env.GetURI(), "/") + "/account/merge/confirm?token=" + url.QueryEscape(token)
	if !ctx.InProduction {
		ctx.Infos.Printf("[dev] account merge link for %s: %s", request.TargetEmail, confirmationURL)
	}
	markdown := fmt.Sprintf("# Add this email to your bitcoin++ account\n\n%s (%s) asked to add %s to their bitcoin++ account. Because this email already has a profile, you will review which profile details to keep before the accounts are combined. Tickets, talks, volunteer work, orders, and other account history move automatically.\n\n[Review and merge accounts](button#%s)\n\nThis link expires in 30 minutes. If you did not expect this request, you can ignore this email or contact hello@btcpp.dev.\n\n— bitcoin++", markdownEmailText(request.RequesterName), markdownEmailText(request.RequesterEmail), markdownEmailText(request.TargetEmail), confirmationURL)
	return sendPersonAccountEmail(ctx, request.TargetEmail, "Confirm your bitcoin++ account merge", markdown, "person-merge-target-"+request.ID+"-"+token[:12])
}

type PersonMergeConfirmationPage struct {
	Request *types.PersonMergeRequest
	Preview *getters.PersonMergePreview
	Fields  []AdminPersonMergeField
	Token   string
	Merged  bool
	Error   string
	Year    uint
}

func PersonMergeConfirmation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	request, err := getters.GetPersonMergeRequestByConfirmationToken(ctx, token)
	page := personMergeConfirmationPage(request, token, "")
	if err != nil {
		page.Error = err.Error()
	} else if request.Status == "merged" {
		page.Merged = true
	} else if request.Status != "awaiting_confirmation" && request.Status != "pending" {
		page.Error = "This account merge request is no longer available."
	} else if request.Status == "awaiting_confirmation" && (request.ConfirmationExpiresAt == nil || time.Now().After(*request.ConfirmationExpiresAt)) {
		page.Error = "This confirmation link has expired. Ask the other account to submit the request again."
	} else {
		preview, previewErr := getters.PreviewPersonMerge(ctx, request.RequesterPersonID, request.TargetPersonID)
		if previewErr != nil {
			page.Error = previewErr.Error()
		} else {
			page.Preview = preview
			page.Fields = selfServicePersonMergeFields(preview)
			if len(preview.Conflicts) > 0 {
				page.Error = "These accounts have conflicting hackathon or judging records that must be resolved before they can be merged."
			}
		}
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
	request, err := getters.GetPersonMergeRequestByConfirmationToken(ctx, token)
	if err != nil {
		renderPersonMergeConfirmation(w, ctx, personMergeConfirmationPage(nil, token, err.Error()))
		return
	}
	if request.Status != "awaiting_confirmation" && request.Status != "pending" {
		renderPersonMergeConfirmation(w, ctx, personMergeConfirmationPage(request, token, "This account merge request is no longer available."))
		return
	}
	preview, err := getters.PreviewPersonMerge(ctx, request.RequesterPersonID, request.TargetPersonID)
	if err != nil {
		renderPersonMergeConfirmation(w, ctx, personMergeConfirmationPage(request, token, err.Error()))
		return
	}
	if len(preview.Conflicts) > 0 {
		page := personMergeConfirmationPage(request, token, "These accounts have conflicting hackathon or judging records that must be resolved before they can be merged.")
		page.Preview = preview
		page.Fields = selfServicePersonMergeFields(preview)
		renderPersonMergeConfirmation(w, ctx, page)
		return
	}
	decisions, err := parseSelfServicePersonMergeDecisions(r, preview)
	if err != nil {
		page := personMergeConfirmationPage(request, token, err.Error())
		page.Preview = preview
		page.Fields = selfServicePersonMergeFields(preview)
		renderPersonMergeConfirmation(w, ctx, page)
		return
	}
	request, _, err = getters.ConfirmPersonMergeRequest(ctx, token)
	if err != nil {
		renderPersonMergeConfirmation(w, ctx, personMergeConfirmationPage(request, token, err.Error()))
		return
	}
	_, err = getters.MergePeople(ctx, getters.PersonMergeInput{
		CanonicalPersonID: request.RequesterPersonID,
		SourcePersonID:    request.TargetPersonID,
		MergedByPersonID:  request.RequesterPersonID,
		MergeRequestID:    request.ID,
		Decisions:         decisions,
	})
	if err != nil {
		ctx.Err.Printf("self-service person merge %s: %s", request.ID, err)
		page := personMergeConfirmationPage(request, token, err.Error())
		page.Preview = preview
		page.Fields = selfServicePersonMergeFields(preview)
		renderPersonMergeConfirmation(w, ctx, page)
		return
	}
	invalidateWhoIsDirectoryCache()
	if err := auth.LoginPersonWithEmail(ctx, r, request.RequesterPersonID, request.TargetEmail); err != nil {
		ctx.Err.Printf("self-service merge login %s: %s", request.ID, err)
	}
	request.Status = "merged"
	page := personMergeConfirmationPage(request, token, "")
	page.Merged = true
	renderPersonMergeConfirmation(w, ctx, page)
}

func sendPersonAccountEmail(ctx *config.AppContext, recipient, subject, markdown, jobKey string) error {
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
	requestPersonEmailAddition(w, r, ctx, id, email)
}

func DashboardPersonEmailResend(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requirePersonIdentity(w, r, ctx)
	if id == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		redirectPersonEmails(w, r, "", "Invalid form submission.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	pending, err := getters.ListPendingPersonEmailVerifications(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/emails resend pending list %s: %s", id.PersonID, err)
		redirectPersonEmails(w, r, "", "Unable to resend that verification email.")
		return
	}
	found := false
	for _, pendingEmail := range pending {
		if strings.EqualFold(pendingEmail, email) {
			found = true
			break
		}
	}
	if !found {
		redirectPersonEmails(w, r, "", "That email no longer has a pending verification request.")
		return
	}
	requestPersonEmailAddition(w, r, ctx, id, email)
}

func requestPersonEmailAddition(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, id *auth.Identity, email string) {
	resolution, err := getters.ResolvePersonByEmail(ctx, email)
	if err != nil {
		redirectPersonEmails(w, r, "", err.Error())
		return
	}
	if resolution.IsConflict() {
		redirectPersonEmails(w, r, "", "That email is attached to duplicate profiles that must be resolved before it can be added.")
		return
	}
	if resolution.Alias != nil && resolution.Person != nil {
		if resolution.Alias.PersonID == id.PersonID {
			redirectPersonEmails(w, r, "", "That email is already attached to your account.")
			return
		}
		request, token, err := getters.CreatePersonMergeRequest(ctx, id.PersonID, email)
		if err != nil {
			redirectPersonEmails(w, r, "", err.Error())
			return
		}
		if err := sendPersonMergeConfirmationEmail(ctx, request, token); err != nil {
			ctx.Err.Printf("person merge request %s confirmation email: %s", request.ID, err)
			redirectPersonEmails(w, r, "Your request was saved.", "The confirmation email could not be scheduled. Submit the address again to retry.")
			return
		}
		redirectPersonEmails(w, r, "That email already has a bitcoin++ profile. We sent it a link to review and combine the accounts.", "")
		return
	}
	token, err := getters.CreatePersonEmailVerification(ctx, id.PersonID, email, false)
	if err != nil {
		redirectPersonEmails(w, r, "", err.Error())
		return
	}
	verificationURL := strings.TrimRight(ctx.Env.GetURI(), "/") + "/dashboard/emails/verify?token=" + url.QueryEscape(token)
	requesterEmail := id.PrimaryEmail
	if requesterEmail == "" {
		requesterEmail = id.LoginEmail
	}
	if err := sendPersonEmailAdditionVerification(ctx, requesterEmail, email, verificationURL, token); err != nil {
		ctx.Err.Printf("/dashboard/emails send verification %s: %s", email, err)
		redirectPersonEmails(w, r, "", "The verification email could not be sent. Try again.")
		return
	}
	redirectPersonEmails(w, r, "Check "+email+" for a verification link. It expires in 30 minutes.", "")
}

func sendPersonEmailAdditionVerification(ctx *config.AppContext, requesterEmail, targetEmail, verificationURL, token string) error {
	if !ctx.InProduction {
		ctx.Infos.Printf("[dev] add account email link for %s: %s", targetEmail, verificationURL)
	}
	markdown := personEmailAdditionVerificationMarkdown(requesterEmail, targetEmail, verificationURL)
	return sendPersonAccountEmail(ctx, targetEmail, "Add This Email to Your bitcoin++ Account", markdown, "person-email-add-"+token[:12])
}

func personEmailAdditionVerificationMarkdown(requesterEmail, targetEmail, verificationURL string) string {
	return fmt.Sprintf("# Add This Email to Your bitcoin++ Account\n\nA request was made from the bitcoin++ account for %s to add %s as another email address on that account.\n\n[Add This Email](button#%s)\n\nOnly click this button if you made or expected this request. If you do not want this email attached to that account, do not click the link—just ignore or delete this message.\n\nThis link expires in 30 minutes.\n\n— bitcoin++", markdownEmailText(requesterEmail), markdownEmailText(targetEmail), verificationURL)
}

var selfServicePersonMergeFieldKeys = map[string]bool{
	"name": true, "photo": true, "phone": true, "signal": true,
	"telegram": true, "twitter": true, "nostr": true, "github": true,
	"instagram": true, "linkedin": true, "leetcode": true, "website": true,
	"company": true, "org_logo": true, "bio": true, "available_to_hire": true,
	"looking_to_hire": true, "tshirt": true, "lightning_address": true,
	"bitcoin_address": true,
}

func selfServicePersonMergeFields(preview *getters.PersonMergePreview) []AdminPersonMergeField {
	page := adminPersonMergePage(preview, "")
	reviewKeys := make(map[string]bool, len(preview.Fields))
	for _, field := range preview.Fields {
		if shouldReviewSelfServicePersonMergeField(field) {
			reviewKeys[field.Spec.Key] = true
		}
	}
	var fields []AdminPersonMergeField
	for _, field := range page.Fields {
		if reviewKeys[field.Key] {
			fields = append(fields, field)
		}
	}
	return fields
}

func parseSelfServicePersonMergeDecisions(r *http.Request, preview *getters.PersonMergePreview) (map[string]getters.PersonMergeDecision, error) {
	decisions := make(map[string]getters.PersonMergeDecision, len(preview.Fields))
	useSourceTaxForm := false
	for _, field := range preview.Fields {
		if field.Spec.Key == "tax_form_object" {
			useSourceTaxForm = mergeValueEmpty(field.Canonical) && !mergeValueEmpty(field.Source)
			break
		}
	}
	for _, field := range preview.Fields {
		choice := "canonical"
		value := field.Canonical
		if shouldReviewSelfServicePersonMergeField(field) {
			choice = strings.TrimSpace(r.FormValue("choice_" + field.Spec.Key))
			switch choice {
			case "canonical":
				value = field.Canonical
			case "source":
				value = field.Source
			default:
				return nil, fmt.Errorf("choose which value to keep for %s", field.Spec.Label)
			}
		} else if strings.HasPrefix(field.Spec.Key, "tax_form_") && useSourceTaxForm {
			choice = "source"
			value = field.Source
		} else if mergeValueEmpty(field.Canonical) && !mergeValueEmpty(field.Source) {
			choice = "source"
			value = field.Source
		}
		decisions[field.Spec.Key] = getters.PersonMergeDecision{Choice: choice, Value: value}
	}
	return decisions, nil
}

func shouldReviewSelfServicePersonMergeField(field getters.PersonMergeField) bool {
	return selfServicePersonMergeFieldKeys[field.Spec.Key] &&
		!mergeValueEmpty(field.Source) && !reflect.DeepEqual(field.Canonical, field.Source)
}

func personMergeConfirmationPage(request *types.PersonMergeRequest, token, message string) *PersonMergeConfirmationPage {
	return &PersonMergeConfirmationPage{Request: request, Token: token, Error: message, Year: helpers.CurrentYear()}
}

func renderPersonMergeConfirmation(w http.ResponseWriter, ctx *config.AppContext, page *PersonMergeConfirmationPage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "person_merge_confirmation.tmpl", page); err != nil {
		ctx.Err.Printf("account merge confirmation render: %s", err)
		http.Error(w, "Unable to render account merge", http.StatusInternalServerError)
	}
}

type PersonEmailVerificationPage struct {
	Email          string
	RequesterEmail string
	Token          string
	Error          string
	Year           uint
}

func DashboardPersonEmailVerify(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		renderPersonEmailVerification(w, ctx, &PersonEmailVerificationPage{
			Error: "This verification URL is incomplete. Open the full link from the email you received; it includes the secure verification token.",
			Year:  helpers.CurrentYear(),
		})
		return
	}
	email, requesterEmail, err := getters.GetPendingPersonEmailVerification(ctx, token)
	page := &PersonEmailVerificationPage{Email: email, RequesterEmail: requesterEmail, Token: token, Year: helpers.CurrentYear()}
	if err != nil {
		page.Error = err.Error()
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "person_email_verification.tmpl", page); err != nil {
		ctx.Err.Printf("email addition verification render: %s", err)
		http.Error(w, "Unable to render email verification", http.StatusInternalServerError)
	}
}

func DashboardPersonEmailVerifyConfirm(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		renderPersonEmailVerification(w, ctx, &PersonEmailVerificationPage{Error: "Invalid form submission.", Year: helpers.CurrentYear()})
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	personID, email, err := getters.ConsumePersonEmailVerification(ctx, token)
	if err != nil {
		renderPersonEmailVerification(w, ctx, &PersonEmailVerificationPage{Token: token, Error: err.Error(), Year: helpers.CurrentYear()})
		return
	}
	if err := auth.LoginPersonWithEmail(ctx, r, personID, email); err != nil {
		ctx.Err.Printf("/dashboard/emails verified login %s: %s", personID, err)
		http.Error(w, "Email verified, but the session could not be updated. Sign in again.", http.StatusInternalServerError)
		return
	}
	redirectPersonEmails(w, r, email+" is now attached to your account.", "")
}

func renderPersonEmailVerification(w http.ResponseWriter, ctx *config.AppContext, page *PersonEmailVerificationPage) {
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "person_email_verification.tmpl", page); err != nil {
		ctx.Err.Printf("email addition verification render: %s", err)
		http.Error(w, "Unable to render email verification", http.StatusInternalServerError)
	}
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
	if err := auth.UpdateSessionEmail(ctx, r, id.PersonID, email); err != nil {
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
		if err := auth.UpdateSessionEmail(ctx, r, id.PersonID, primary); err != nil {
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
