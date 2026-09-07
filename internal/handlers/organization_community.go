package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

type OrganizationApplicationAdminPage struct {
	Application  *types.OrganizationApplication
	FlashMessage string
	FlashError   string
	Year         uint
}

func OrganizationMembershipRequestCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	request, autoApproved, err := getters.CreateOrganizationMembershipRequest(ctx, organizationID, id.PersonID, r.FormValue("message"))
	if err != nil {
		message := err.Error()
		if errors.Is(err, getters.ErrOrganizationMembershipRequestPending) {
			message = "Your membership request is already waiting for review."
		}
		http.Redirect(w, r, "/dashboard/orgs?error="+url.QueryEscape(message), http.StatusSeeOther)
		return
	}
	action := "organization.membership_requested"
	flash := "Your membership request was sent to the organization managers."
	if autoApproved {
		action = "organization.membership_auto_approved"
		flash = "You joined " + request.OrganizationName + "."
	}
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, action, "organization_membership_request", request.ID, nil)
	http.Redirect(w, r, "/dashboard/orgs?flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

func OrganizationMembershipPolicyUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, organizationID, destination, ok := organizationManagerMutation(w, r, ctx)
	if !ok {
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	policy := strings.ToLower(strings.TrimSpace(r.FormValue("membership_policy")))
	if err := getters.UpdateOrganizationMembershipPolicy(ctx, organizationID, policy); err != nil {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.membership_policy_updated", "organization", organizationID, map[string]any{"policy": policy})
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape("Membership request policy updated."), http.StatusSeeOther)
}

func OrganizationMembershipRequestReview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, organizationID, destination, ok := organizationManagerMutation(w, r, ctx)
	if !ok {
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	decision := strings.ToLower(strings.TrimSpace(r.FormValue("decision")))
	requestID := strings.TrimSpace(mux.Vars(r)["requestID"])
	request, err := getters.ReviewOrganizationMembershipRequest(ctx, organizationID, requestID, id.PersonID, decision, r.FormValue("review_note"))
	if err != nil {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.membership_request_"+decision, "organization_membership_request", request.ID, map[string]any{"person_id": request.PersonID})
	label := "denied"
	if decision == "approved" {
		label = "approved and added as a member"
	}
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape("Membership request "+label+"."), http.StatusSeeOther)
}

func OrganizationApplicationCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	applicantEmail := strings.TrimSpace(id.PrimaryEmail)
	if applicantEmail == "" {
		applicantEmail = strings.TrimSpace(id.LoginEmail)
	}
	application := &types.OrganizationApplication{
		SubmittedByPersonID: id.PersonID,
		ApplicantEmail:      applicantEmail,
		Name:                r.FormValue("name"),
		Tagline:             r.FormValue("tagline"),
		ContactEmail:        r.FormValue("contact_email"),
		Website:             r.FormValue("website"),
		Github:              r.FormValue("github"),
		Notes:               r.FormValue("notes"),
	}
	if err := getters.CreateOrganizationApplication(ctx, application); err != nil {
		message := err.Error()
		if errors.Is(err, getters.ErrOrganizationApplicationPending) {
			message = "That organization already exists or has an application awaiting review."
		}
		http.Redirect(w, r, "/dashboard/orgs?error="+url.QueryEscape(message), http.StatusSeeOther)
		return
	}
	admins, err := getters.ListSpeakersWithRole(ctx, "global-admin")
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs application %s global admins: %s", application.ID, err)
	} else {
		reviewURL := ctx.Env.GetURI() + "/admin/org-applications/" + url.PathEscape(application.ID)
		for _, admin := range admins {
			if admin == nil || strings.TrimSpace(admin.Email) == "" {
				continue
			}
			if sendErr := emails.SendOrganizationApplicationAdminNotice(ctx, application, admin.Email, reviewURL); sendErr != nil {
				ctx.Err.Printf("/dashboard/orgs application %s notify %s: %s", application.ID, admin.Email, sendErr)
			}
		}
	}
	http.Redirect(w, r, "/dashboard/orgs?flash="+url.QueryEscape("Organization application submitted. Global administrators have been notified."), http.StatusSeeOther)
}

func OrganizationApplicationAdminList(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	http.Redirect(w, r, "/admin/orgs#applications", http.StatusSeeOther)
}

func OrganizationApplicationAdminDetail(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	application, err := getters.GetOrganizationApplication(ctx, strings.TrimSpace(mux.Vars(r)["applicationID"]))
	if err != nil || application == nil {
		handle404(w, r, ctx)
		return
	}
	page := &OrganizationApplicationAdminPage{Application: application, FlashMessage: r.URL.Query().Get("flash"), FlashError: r.URL.Query().Get("error"), Year: helpers.CurrentYear()}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/organization_application.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/org-applications/%s template: %s", application.ID, err)
		http.Error(w, "Unable to load organization application", http.StatusInternalServerError)
	}
}

func OrganizationApplicationAdminReview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireGlobalAdmin(w, r, ctx)
	if id == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	applicationID := strings.TrimSpace(mux.Vars(r)["applicationID"])
	edited := &types.OrganizationApplication{
		Name: r.FormValue("name"), Tagline: r.FormValue("tagline"),
		ContactEmail: r.FormValue("contact_email"), Website: r.FormValue("website"),
		Github: r.FormValue("github"), Notes: r.FormValue("notes"), ReviewNote: r.FormValue("review_note"),
	}
	decision := strings.ToLower(strings.TrimSpace(r.FormValue("decision")))
	application, err := getters.ReviewOrganizationApplication(ctx, applicationID, id.PersonID, decision, edited)
	if err != nil {
		http.Redirect(w, r, "/admin/org-applications/"+url.PathEscape(applicationID)+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	resultURL := ctx.Env.GetURI() + "/dashboard/orgs"
	if application.OrganizationID != "" {
		resultURL = ctx.Env.GetURI() + "/dashboard/orgs/" + url.PathEscape(application.OrganizationID)
	}
	if err := emails.SendOrganizationApplicationDecision(ctx, application, resultURL); err != nil {
		ctx.Err.Printf("/admin/org-applications/%s decision email: %s", application.ID, err)
		http.Redirect(w, r, "/admin/org-applications/"+url.PathEscape(application.ID)+"?flash="+url.QueryEscape("Application "+decision+".")+"&error="+url.QueryEscape("The applicant notification email could not be sent."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/org-applications/"+url.PathEscape(application.ID)+"?flash="+url.QueryEscape("Application "+decision+" and applicant notified."), http.StatusSeeOther)
}
