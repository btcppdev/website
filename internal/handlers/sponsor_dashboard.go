package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/missives"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

type SponsorDashboardPage struct {
	Memberships     []*types.OrganizationMembership
	Membership      *types.OrganizationMembership
	Organization    *types.Org
	Upcoming        []*types.SponsorDashboardEvent
	Past            []*types.SponsorDashboardEvent
	Members         []*types.OrganizationMembership
	PrizeProposals  []*types.SponsorAwardProposal
	TicketIssuances []*types.SponsorTicketIssuance
	CanManage       bool
	CanEditOrg      bool
	SpacesReady     bool
	CSRF            string
	FlashMessage    string
	FlashError      string
	InviteLink      string
	Year            uint
}

func (p *SponsorDashboardPage) ProposalsFor(sponsorshipID string) []*types.SponsorAwardProposal {
	var out []*types.SponsorAwardProposal
	for _, proposal := range p.PrizeProposals {
		if proposal != nil && proposal.SponsorshipID == sponsorshipID {
			out = append(out, proposal)
		}
	}
	return out
}

func (p *SponsorDashboardPage) TicketIssuancesFor(sponsorshipID string) []*types.SponsorTicketIssuance {
	var out []*types.SponsorTicketIssuance
	for _, issuance := range p.TicketIssuances {
		if issuance != nil && issuance.SponsorshipID == sponsorshipID {
			out = append(out, issuance)
		}
	}
	return out
}

func (p *SponsorDashboardPage) TicketsRemaining(event *types.SponsorDashboardEvent) int {
	if event == nil || event.Entitlement == nil {
		return 0
	}
	remaining := event.Entitlement.TicketAllocation - event.TicketsIssued
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (p *SponsorDashboardPage) TicketBatchMaximum(event *types.SponsorDashboardEvent) int {
	remaining := p.TicketsRemaining(event)
	if remaining > compTicketMaxCount {
		return compTicketMaxCount
	}
	return remaining
}

func sponsorDashboardPercent(used, total int) int {
	if total <= 0 || used <= 0 {
		return 0
	}
	percent := used * 100 / total
	if percent > 100 {
		return 100
	}
	return percent
}

func (p *SponsorDashboardPage) TicketUsePercent(event *types.SponsorDashboardEvent) int {
	if event == nil || event.Entitlement == nil {
		return 0
	}
	return sponsorDashboardPercent(event.TicketsIssued, event.Entitlement.TicketAllocation)
}

func (p *SponsorDashboardPage) ProposalUsePercent(event *types.SponsorDashboardEvent) int {
	if event == nil || event.Entitlement == nil {
		return 0
	}
	return sponsorDashboardPercent(event.AwardCount, event.Entitlement.SponsorAwardLimit)
}

func (p *SponsorDashboardPage) ProposalSlotsRemaining(event *types.SponsorDashboardEvent) int {
	if event == nil || event.Entitlement == nil || event.Sponsorship == nil {
		return 0
	}
	used := event.AwardCount
	for _, proposal := range p.PrizeProposals {
		if proposal != nil && proposal.SponsorshipID == event.Sponsorship.Ref && proposal.Status == "pending" {
			used++
		}
	}
	remaining := event.Entitlement.SponsorAwardLimit - used
	if remaining < 0 {
		return 0
	}
	return remaining
}

const sponsorInviteLinkSessionKey = "sponsor_invite_link"

type SponsorInvitePage struct {
	Invite *types.OrganizationMemberInvite
	Token  string
	CSRF   string
	Error  string
	Year   uint
}

func SponsorDashboardIndex(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, memberships, ok := sponsorDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	if len(memberships) == 0 {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("You do not manage a sponsor organization yet."), http.StatusSeeOther)
		return
	}
	_ = id
	http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(memberships[0].OrganizationID), http.StatusSeeOther)
}

func SponsorDashboard(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	_, memberships, ok := sponsorDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	membership := organizationMembershipByID(memberships, organizationID)
	if membership == nil {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("You do not have access to that sponsor organization."), http.StatusSeeOther)
		return
	}
	events, err := getters.ListSponsorDashboardEvents(ctx, organizationID)
	if err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s events: %s", organizationID, err)
		http.Error(w, "Unable to load sponsor dashboard", http.StatusInternalServerError)
		return
	}
	members, err := getters.ListOrganizationMembers(ctx, organizationID)
	if err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s members: %s", organizationID, err)
		http.Error(w, "Unable to load sponsor team", http.StatusInternalServerError)
		return
	}
	proposals, err := getters.ListSponsorAwardProposalsForOrganization(ctx, organizationID)
	if err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s proposals: %s", organizationID, err)
		http.Error(w, "Unable to load sponsor prize proposals", http.StatusInternalServerError)
		return
	}
	issuances, err := getters.ListSponsorTicketIssuances(ctx, organizationID)
	if err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s ticket issuances: %s", organizationID, err)
		http.Error(w, "Unable to load sponsor tickets", http.StatusInternalServerError)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to prepare sponsor dashboard", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	var upcoming, past []*types.SponsorDashboardEvent
	canEditOrg := false
	for _, event := range events {
		if event == nil || event.Conference == nil {
			continue
		}
		if event.Sponsorship != nil && event.Entitlement != nil && event.Entitlement.CanEditOrganization && sponsorStatusGrantsCapabilities(event.Sponsorship.Status) && !event.Conference.EndDate.Before(now) {
			canEditOrg = true
		}
		if !event.Conference.EndDate.IsZero() && event.Conference.EndDate.Before(now) {
			past = append(past, event)
		} else {
			upcoming = append(upcoming, event)
		}
	}
	sort.SliceStable(upcoming, func(i, j int) bool {
		return upcoming[i].Conference.StartDate.Before(upcoming[j].Conference.StartDate)
	})

	page := &SponsorDashboardPage{
		Memberships:     memberships,
		Membership:      membership,
		Organization:    membership.Organization,
		Upcoming:        upcoming,
		Past:            past,
		Members:         members,
		PrizeProposals:  proposals,
		TicketIssuances: issuances,
		CanManage:       sponsorMembershipCanManage(membership),
		CanEditOrg:      sponsorMembershipCanManage(membership) && canEditOrg,
		SpacesReady:     spaces.IsConfigured(),
		CSRF:            csrf,
		FlashMessage:    r.URL.Query().Get("flash"),
		FlashError:      r.URL.Query().Get("error"),
		InviteLink:      ctx.Session.PopString(r.Context(), sponsorInviteLinkSessionKey),
		Year:            helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_sponsor.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s template: %s", organizationID, err)
		http.Error(w, "Unable to load sponsor dashboard", http.StatusInternalServerError)
	}
}

func SponsorDashboardPrizeProposalCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, memberships, ok := sponsorDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	redirectTo := "/dashboard/sponsor/" + url.PathEscape(organizationID)
	membership := organizationMembershipByID(memberships, organizationID)
	if membership == nil || !sponsorMembershipCanManage(membership) {
		http.Redirect(w, r, redirectTo+"?error="+url.QueryEscape("Only organization owners and managers can propose sponsor prizes."), http.StatusSeeOther)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form token", http.StatusBadRequest)
		return
	}
	maxAwardees, err := strconv.Atoi(strings.TrimSpace(r.FormValue("MaxAwardees")))
	if err != nil {
		maxAwardees = 0
	}
	proposal, err := getters.CreateSponsorAwardProposal(ctx, getters.SponsorAwardProposalInput{
		SponsorshipID:       r.FormValue("SponsorshipID"),
		ConferenceID:        r.FormValue("ConferenceID"),
		CompetitionID:       r.FormValue("CompetitionID"),
		OrganizationID:      organizationID,
		SubmittedByPersonID: id.PersonID,
		Title:               r.FormValue("Title"), Description: r.FormValue("Description"),
		JudgingInstructions: r.FormValue("JudgingInstructions"), MaxAwardees: maxAwardees,
		OptInRequired: r.FormValue("OptInRequired") == "on",
		FinalistsOnly: r.FormValue("FinalistsOnly") == "on",
		PrizeType:     r.FormValue("PrizeType"), PrizeTitle: r.FormValue("PrizeTitle"),
		PrizeDescription: r.FormValue("PrizeDescription"), PrizeValueText: r.FormValue("PrizeValueText"),
	})
	if err != nil {
		http.Redirect(w, r, redirectTo+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.RecordSponsorAuditEvent(ctx, organizationID, proposal.SponsorshipID,
		proposal.ConferenceID, id.PersonID, "sponsor.award_proposed",
		"sponsor_award_proposal", proposal.ID, nil); err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s proposal audit: %s", organizationID, err)
	}
	http.Redirect(w, r, redirectTo+"?flash="+url.QueryEscape("Prize proposal sent to the hackathon organizers for approval."), http.StatusSeeOther)
}

func SponsorDashboardTicketsIssue(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, memberships, ok := sponsorDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	redirectTo := "/dashboard/sponsor/" + url.PathEscape(organizationID)
	membership := organizationMembershipByID(memberships, organizationID)
	if membership == nil || !sponsorMembershipCanManage(membership) {
		http.Redirect(w, r, redirectTo+"?error="+url.QueryEscape("Only organization owners and managers can issue sponsor tickets."), http.StatusSeeOther)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form token", http.StatusBadRequest)
		return
	}
	quantity, err := strconv.Atoi(strings.TrimSpace(r.FormValue("Quantity")))
	if err != nil {
		quantity = 0
	}
	issuance, err := getters.IssueSponsorTickets(ctx, organizationID,
		r.FormValue("SponsorshipID"), r.FormValue("ConferenceID"),
		id.PersonID, r.FormValue("Email"), quantity)
	if err != nil {
		http.Redirect(w, r, redirectTo+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := missives.NewTicketSub(ctx, issuance.RecipientEmail, issuance.ConferenceTag, "sponsor", false); err != nil {
		ctx.Err.Printf("/%s sponsor ticket newsletter sub for %s: %s", issuance.ConferenceTag, issuance.RecipientEmail, err)
	}
	if err := getters.RecordSponsorAuditEvent(ctx, organizationID, issuance.SponsorshipID,
		issuance.ConferenceID, id.PersonID, "sponsor.tickets_issued",
		"sponsor_ticket_issuance", issuance.ID,
		map[string]any{"recipient_email": issuance.RecipientEmail, "quantity": issuance.Quantity}); err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s ticket audit: %s", organizationID, err)
	}
	message := fmt.Sprintf("Issued %d sponsor ticket(s) to %s. The ticket email will be sent shortly.", issuance.Quantity, issuance.RecipientEmail)
	http.Redirect(w, r, redirectTo+"?flash="+url.QueryEscape(message), http.StatusSeeOther)
}

func SponsorDashboardInviteCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, memberships, ok := sponsorDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	redirectTo := "/dashboard/sponsor/" + url.PathEscape(organizationID)
	membership := organizationMembershipByID(memberships, organizationID)
	if membership == nil || !sponsorMembershipCanManage(membership) {
		http.Redirect(w, r, redirectTo+"?error="+url.QueryEscape("Only organization owners and managers can invite teammates."), http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form token", http.StatusBadRequest)
		return
	}
	token, invite, err := getters.CreateOrganizationMemberInvite(ctx, organizationID, r.FormValue("email"), r.FormValue("role"), id.PersonID, time.Now().Add(72*time.Hour))
	if err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s invite: %s", organizationID, err)
		http.Redirect(w, r, redirectTo+"?error="+url.QueryEscape("The teammate invitation could not be created."), http.StatusSeeOther)
		return
	}
	ctx.Session.Put(r.Context(), sponsorInviteLinkSessionKey, ctx.Env.GetURI()+"/sponsor-invites/"+url.PathEscape(token))
	if err := getters.RecordSponsorAuditEvent(ctx, organizationID, "", "", id.PersonID, "organization.member_invited", "organization_member_invite", invite.ID, map[string]any{"email": invite.Email, "role": invite.Role}); err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s invite audit: %s", organizationID, err)
	}
	http.Redirect(w, r, redirectTo+"?flash="+url.QueryEscape("Invitation created. Copy the secure link below; it expires in 72 hours."), http.StatusSeeOther)
}

func SponsorInvite(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	protectSponsorInviteResponse(w)
	token := strings.TrimSpace(mux.Vars(r)["token"])
	invite, err := getters.GetOrganizationMemberInviteByToken(ctx, token)
	if err != nil {
		http.Error(w, "Unable to load invitation", http.StatusInternalServerError)
		return
	}
	if invite == nil {
		http.Error(w, "Invitation not found", http.StatusNotFound)
		return
	}
	id, err := auth.Resolve(r, ctx)
	if err != nil {
		http.Error(w, "Unable to resolve account", http.StatusInternalServerError)
		return
	}
	if id == nil || strings.TrimSpace(id.PersonID) == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to prepare invitation", http.StatusInternalServerError)
		return
	}
	page := &SponsorInvitePage{Invite: invite, Token: token, CSRF: csrf, Error: r.URL.Query().Get("error"), Year: helpers.CurrentYear()}
	if invite.AcceptedAt != nil {
		page.Error = "This invitation has already been used."
	} else if invite.RevokedAt != nil || !invite.ExpiresAt.After(time.Now()) {
		page.Error = "This invitation is no longer valid. Ask an organization manager for a new one."
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "sponsor_invite.tmpl", page); err != nil {
		ctx.Err.Printf("sponsor invite template: %s", err)
		http.Error(w, "Unable to load invitation", http.StatusInternalServerError)
	}
}

func SponsorInviteAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	protectSponsorInviteResponse(w)
	token := strings.TrimSpace(mux.Vars(r)["token"])
	id, err := auth.Resolve(r, ctx)
	if err != nil || id == nil || strings.TrimSpace(id.PersonID) == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form token", http.StatusBadRequest)
		return
	}
	invite, err := getters.AcceptOrganizationMemberInvite(ctx, token, id.PersonID)
	if err != nil {
		ctx.Err.Printf("sponsor invite acceptance: %s", err)
		http.Redirect(w, r, "/sponsor-invites/"+url.PathEscape(token)+"?error="+url.QueryEscape("This invitation could not be accepted. Make sure you are signed in with the invited verified email."), http.StatusSeeOther)
		return
	}
	if err := getters.RecordSponsorAuditEvent(ctx, invite.OrganizationID, "", "", id.PersonID, "organization.member_invite_accepted", "organization_member_invite", invite.ID, nil); err != nil {
		ctx.Err.Printf("sponsor invite acceptance audit: %s", err)
	}
	http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(invite.OrganizationID)+"?flash="+url.QueryEscape("You joined the sponsor organization."), http.StatusSeeOther)
}

func SponsorDashboardProfileUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, memberships, ok := sponsorDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	membership := organizationMembershipByID(memberships, organizationID)
	if membership == nil || !sponsorMembershipCanManage(membership) {
		http.Redirect(w, r, "/dashboard/sponsor?error="+url.QueryEscape("Only organization owners and managers can edit the sponsor profile."), http.StatusSeeOther)
		return
	}
	events, err := getters.ListSponsorDashboardEvents(ctx, organizationID)
	if err != nil {
		http.Error(w, "Unable to verify sponsorship", http.StatusInternalServerError)
		return
	}
	canEdit := false
	for _, event := range events {
		if event != nil && event.Conference != nil && event.Sponsorship != nil && !event.Conference.EndDate.Before(time.Now()) && event.Entitlement != nil && event.Entitlement.CanEditOrganization && sponsorStatusGrantsCapabilities(event.Sponsorship.Status) {
			canEdit = true
			break
		}
	}
	if !canEdit {
		http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(organizationID)+"?error="+url.QueryEscape("This sponsorship does not include organization profile editing."), http.StatusSeeOther)
		return
	}

	limitRequestBody(w, r, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	if !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form token", http.StatusBadRequest)
		return
	}
	org, err := getters.GetOrg(ctx, organizationID)
	if err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}
	org.Name = strings.TrimSpace(r.FormValue("Name"))
	org.Tagline = strings.TrimSpace(r.FormValue("Tagline"))
	org.Email = strings.TrimSpace(r.FormValue("Email"))
	org.Website = strings.TrimSpace(r.FormValue("Website"))
	org.LinkedIn = strings.TrimSpace(r.FormValue("LinkedIn"))
	org.Instagram = strings.TrimSpace(r.FormValue("Instagram"))
	org.Youtube = strings.TrimSpace(r.FormValue("Youtube"))
	org.Github = strings.TrimSpace(r.FormValue("Github"))
	org.Twitter = types.ParseTwitter(r.FormValue("Twitter"))
	org.Nostr = strings.TrimSpace(r.FormValue("Nostr"))
	org.Matrix = strings.TrimSpace(r.FormValue("Matrix"))
	org.Hiring = r.FormValue("Hiring") == "on"
	org.LogoLight = strings.TrimSpace(r.FormValue("LogoLight"))
	org.LogoDark = strings.TrimSpace(r.FormValue("LogoDark"))
	if org.Name == "" {
		http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(organizationID)+"?error="+url.QueryEscape("Organization name is required."), http.StatusSeeOther)
		return
	}

	for field, target := range map[string]*string{"LogoLightFile": &org.LogoLight, "LogoDarkFile": &org.LogoDark} {
		raw, contentType, ext, fileErr := readMultipartLogoFile(r, field)
		if fileErr == http.ErrMissingFile {
			continue
		}
		if fileErr != nil {
			http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(organizationID)+"?error="+url.QueryEscape("The logo upload could not be read."), http.StatusSeeOther)
			return
		}
		logoURL, uploadErr := uploadSponsorDashboardLogo(ctx, raw, contentType, ext)
		if uploadErr != nil {
			ctx.Err.Printf("/dashboard/sponsor/%s logo: %s", organizationID, uploadErr)
			http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(organizationID)+"?error="+url.QueryEscape(uploadErr.Error()), http.StatusSeeOther)
			return
		}
		*target = logoURL
	}
	if err := getters.UpdateOrganizationPublicDetails(ctx, org); err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s profile: %s", organizationID, err)
		http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(organizationID)+"?error="+url.QueryEscape("Organization profile could not be saved."), http.StatusSeeOther)
		return
	}
	if err := getters.RecordSponsorAuditEvent(ctx, organizationID, "", "", id.PersonID, "organization.profile_updated", "organization", organizationID, nil); err != nil {
		ctx.Err.Printf("/dashboard/sponsor/%s audit: %s", organizationID, err)
	}
	http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(organizationID)+"?flash="+url.QueryEscape("Organization profile updated."), http.StatusSeeOther)
}

func sponsorDashboardIdentity(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*auth.Identity, []*types.OrganizationMembership, bool) {
	id, err := auth.Resolve(r, ctx)
	if err != nil {
		ctx.Err.Printf("sponsor dashboard identity: %s", err)
		http.Error(w, "Unable to resolve account", http.StatusInternalServerError)
		return nil, nil, false
	}
	if id == nil || strings.TrimSpace(id.PersonID) == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return nil, nil, false
	}
	memberships, err := getters.ListOrganizationMembershipsForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("sponsor dashboard memberships for %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load sponsor access", http.StatusInternalServerError)
		return nil, nil, false
	}
	return id, memberships, true
}

func organizationMembershipByID(memberships []*types.OrganizationMembership, organizationID string) *types.OrganizationMembership {
	for _, membership := range memberships {
		if membership != nil && membership.OrganizationID == organizationID {
			return membership
		}
	}
	return nil
}

func sponsorMembershipCanManage(membership *types.OrganizationMembership) bool {
	return membership != nil && (membership.Role == getters.OrganizationRoleOwner || membership.Role == getters.OrganizationRoleManager)
}

func sponsorStatusGrantsCapabilities(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "paid", "committed":
		return true
	default:
		return false
	}
}

func protectSponsorInviteResponse(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func uploadSponsorDashboardLogo(ctx *config.AppContext, raw []byte, contentType, ext string) (string, error) {
	if !spaces.IsConfigured() {
		return "", fmt.Errorf("Logo uploads are not configured. Use an existing logo URL for now.")
	}
	shortID := imgproc.ShortID(raw)
	key := "sponsors/" + shortID + ext
	if !spaces.Exists(key) {
		if _, err := spaces.Upload(key, raw, contentType, ""); err != nil {
			return "", fmt.Errorf("logo upload failed: %w", err)
		}
	}
	newPhotoPipeline(ctx).updateOrgLogoManifest(key, raw)
	return spaces.PublicURL(key), nil
}
