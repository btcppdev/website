package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

const organizationInviteLinkSessionKey = "organization_invite_link"

type OrganizationDashboardIndexPage struct {
	Memberships           []*types.OrganizationMembership
	Directory             []*types.OrganizationDirectoryEntry
	PendingInvites        []*types.OrganizationMemberInvite
	Applications          []*types.OrganizationApplication
	PendingOrgInviteCount int
	HasHackathonProjects  bool
	ShowSponsors          bool
	SpacesReady           bool
	ManagedCount          int
	IsGlobalAdmin         bool
	CSRF                  string
	FlashMessage          string
	FlashError            string
	Year                  uint
}

type OrganizationDashboardPage struct {
	Memberships           []*types.OrganizationMembership
	Membership            *types.OrganizationMembership
	Organization          *types.Org
	Members               []*types.OrganizationMembership
	PendingInvites        []*types.OrganizationMemberInvite
	PendingRequests       []*types.OrganizationMembershipRequest
	SponsorEvents         []*types.SponsorDashboardEvent
	CanManage             bool
	IsOwner               bool
	IsGlobalAdmin         bool
	PendingOrgInviteCount int
	HasHackathonProjects  bool
	ShowSponsors          bool
	SpacesReady           bool
	CSRF                  string
	InviteLink            string
	InviteEmail           string
	FlashMessage          string
	FlashError            string
	Year                  uint
}

type OrganizationDirectoryPage struct {
	Directory             []*types.OrganizationDirectoryEntry
	Search                string
	PendingOrgInviteCount int
	HasHackathonProjects  bool
	ShowSponsors          bool
	IsGlobalAdmin         bool
	CSRF                  string
	Year                  uint
}

func (p *OrganizationDashboardPage) CanRemoveMember(target *types.OrganizationMembership) bool {
	if p == nil || p.Membership == nil || target == nil || target.Status == "removed" {
		return false
	}
	owners := 0
	for _, member := range p.Members {
		if member != nil && member.Status != "removed" && member.Role == getters.OrganizationRoleOwner {
			owners++
		}
	}
	if target.Role == getters.OrganizationRoleOwner && owners <= 1 {
		return false
	}
	if p.Membership.PersonID == target.PersonID {
		return true
	}
	if p.Membership.Role == getters.OrganizationRoleOwner {
		return true
	}
	return p.Membership.Role == getters.OrganizationRoleManager && target.Role == getters.OrganizationRoleMember
}

func OrganizationDashboardIndex(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, memberships, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	managed := 0
	for _, membership := range memberships {
		if sponsorMembershipCanManage(membership) {
			managed++
		}
	}
	pendingInvites, err := getters.ListPendingOrganizationMemberInvitesForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs pending invitations for %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load organization invitations", http.StatusInternalServerError)
		return
	}
	directory, err := getters.ListOrganizationDirectoryForPersonFiltered(ctx, id.PersonID, "", 8)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs directory for %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load organization directory", http.StatusInternalServerError)
		return
	}
	applications, err := getters.ListOrganizationApplicationsForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs applications for %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load organization applications", http.StatusInternalServerError)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to prepare organizations", http.StatusInternalServerError)
		return
	}
	hasHackathonProjects, err := getters.HasHackathonParticipantProjectsForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs hackathon projects for %s: %s", id.PersonID, err)
	}
	page := &OrganizationDashboardIndexPage{
		Memberships: memberships, Directory: directory, PendingInvites: pendingInvites,
		Applications:          applications,
		PendingOrgInviteCount: len(pendingInvites), ManagedCount: managed,
		HasHackathonProjects: hasHackathonProjects,
		ShowSponsors:         hasManagedSponsorOrganization(ctx, id.PersonID, "/dashboard/orgs"),
		SpacesReady:          spaces.IsConfigured(),
		IsGlobalAdmin:        id.IsGlobalAdmin(), CSRF: csrf,
		FlashMessage: r.URL.Query().Get("flash"), FlashError: r.URL.Query().Get("error"),
		Year: helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_orgs.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/orgs template: %s", err)
		http.Error(w, "Unable to load organizations", http.StatusInternalServerError)
	}
}

func OrganizationDashboardDirectory(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	directory, err := getters.ListOrganizationDirectoryForPersonFiltered(ctx, id.PersonID, search, 0)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs/discover directory for %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load organization directory", http.StatusInternalServerError)
		return
	}
	pendingInvites, err := getters.ListPendingOrganizationMemberInvitesForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs/discover pending invitations for %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load organization invitations", http.StatusInternalServerError)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to prepare organization directory", http.StatusInternalServerError)
		return
	}
	hasHackathonProjects, err := getters.HasHackathonParticipantProjectsForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs/discover hackathon projects for %s: %s", id.PersonID, err)
	}
	page := &OrganizationDirectoryPage{
		Directory: directory, Search: search, PendingOrgInviteCount: len(pendingInvites),
		HasHackathonProjects: hasHackathonProjects,
		ShowSponsors:         hasManagedSponsorOrganization(ctx, id.PersonID, "/dashboard/orgs/discover"),
		IsGlobalAdmin:        id.IsGlobalAdmin(), CSRF: csrf, Year: helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_org_discover.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/orgs/discover template: %s", err)
		http.Error(w, "Unable to load organization directory", http.StatusInternalServerError)
	}
}

func OrganizationDashboardInviteAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	if strings.TrimSpace(id.PersonID) == "" {
		http.Redirect(w, r, "/dashboard/orgs?error="+url.QueryEscape("A person profile is required to accept an organization invitation."), http.StatusSeeOther)
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	inviteID := strings.TrimSpace(mux.Vars(r)["inviteID"])
	invite, err := getters.AcceptOrganizationMemberInviteByID(ctx, inviteID, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs invitation %s acceptance: %s", inviteID, err)
		http.Redirect(w, r, "/dashboard/orgs?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	recordOrganizationDashboardAudit(ctx, invite.OrganizationID, id.PersonID, "organization.member_invite_accepted", "organization_member_invite", invite.ID, nil)
	http.Redirect(w, r, "/dashboard/orgs/"+url.PathEscape(invite.OrganizationID)+"?flash="+url.QueryEscape("You joined the organization."), http.StatusSeeOther)
}

func pendingOrganizationInviteCount(ctx *config.AppContext, personID, logContext string) int {
	count, err := getters.CountPendingOrganizationMemberInvitesForPerson(ctx, personID)
	if err != nil {
		ctx.Err.Printf("%s pending organization invitations: %s", logContext, err)
		return 0
	}
	return count
}

func OrganizationDashboard(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, memberships, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	membership := organizationMembershipByID(memberships, organizationID)
	if membership == nil {
		http.Redirect(w, r, "/dashboard/orgs?error="+url.QueryEscape("You are not a member of that organization."), http.StatusSeeOther)
		return
	}
	members, err := getters.ListOrganizationMembers(ctx, organizationID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs/%s members: %s", organizationID, err)
		http.Error(w, "Unable to load organization members", http.StatusInternalServerError)
		return
	}
	canManage := sponsorMembershipCanManage(membership)
	var pendingInvites []*types.OrganizationMemberInvite
	if canManage {
		pendingInvites, err = getters.ListPendingOrganizationMemberInvites(ctx, organizationID)
		if err != nil {
			ctx.Err.Printf("/dashboard/orgs/%s invitations: %s", organizationID, err)
			http.Error(w, "Unable to load organization invitations", http.StatusInternalServerError)
			return
		}
	}
	sponsorEvents, err := getters.ListSponsorDashboardEvents(ctx, organizationID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs/%s sponsorships: %s", organizationID, err)
		http.Error(w, "Unable to load organization sponsorships", http.StatusInternalServerError)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "Unable to prepare organization dashboard", http.StatusInternalServerError)
		return
	}
	hasHackathonProjects, projectsErr := getters.HasHackathonParticipantProjectsForPerson(ctx, id.PersonID)
	if projectsErr != nil {
		ctx.Err.Printf("/dashboard/orgs/%s hackathon projects for %s: %s", organizationID, id.PersonID, projectsErr)
	}
	var pendingRequests []*types.OrganizationMembershipRequest
	if canManage {
		pendingRequests, err = getters.ListPendingOrganizationMembershipRequests(ctx, organizationID)
		if err != nil {
			ctx.Err.Printf("/dashboard/orgs/%s membership requests: %s", organizationID, err)
			http.Error(w, "Unable to load membership requests", http.StatusInternalServerError)
			return
		}
	}
	page := &OrganizationDashboardPage{
		Memberships: memberships, Membership: membership, Organization: membership.Organization,
		Members: members, PendingInvites: pendingInvites, PendingRequests: pendingRequests, SponsorEvents: sponsorEvents,
		CanManage: canManage, IsOwner: membership.Role == getters.OrganizationRoleOwner,
		IsGlobalAdmin: id.IsGlobalAdmin(), SpacesReady: spaces.IsConfigured(), CSRF: csrf,
		PendingOrgInviteCount: pendingOrganizationInviteCount(ctx, id.PersonID, "/dashboard/orgs/"+organizationID),
		HasHackathonProjects:  hasHackathonProjects,
		ShowSponsors:          hasManagedSponsorOrganization(ctx, id.PersonID, "/dashboard/orgs/"+organizationID),
		InviteLink:            ctx.Session.PopString(r.Context(), organizationInviteLinkSessionKey),
		InviteEmail:           ctx.Session.PopString(r.Context(), organizationInviteLinkSessionKey+"_email"),
		FlashMessage:          r.URL.Query().Get("flash"), FlashError: r.URL.Query().Get("error"),
		Year: helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_org.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/orgs/%s template: %s", organizationID, err)
		http.Error(w, "Unable to load organization", http.StatusInternalServerError)
	}
}

func OrganizationDashboardProfileUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, membership, organizationID, destination, ok := organizationManagerMutation(w, r, ctx)
	if !ok {
		return
	}
	limitRequestBody(w, r, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form", http.StatusBadRequest)
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
	if org.Name == "" {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape("Organization name is required."), http.StatusSeeOther)
		return
	}
	for field, target := range map[string]*string{"LogoLightFile": &org.LogoLight, "LogoDarkFile": &org.LogoDark} {
		raw, contentType, ext, fileErr := readMultipartLogoFile(r, field)
		if fileErr == http.ErrMissingFile {
			continue
		}
		if fileErr != nil {
			http.Redirect(w, r, destination+"?error="+url.QueryEscape("The logo upload could not be read."), http.StatusSeeOther)
			return
		}
		logoURL, uploadErr := uploadSponsorDashboardLogo(ctx, raw, contentType, ext)
		if uploadErr != nil {
			ctx.Err.Printf("/dashboard/orgs/%s logo: %s", organizationID, uploadErr)
			http.Redirect(w, r, destination+"?error="+url.QueryEscape(uploadErr.Error()), http.StatusSeeOther)
			return
		}
		*target = logoURL
	}
	if err := getters.UpdateOrganizationPublicDetails(ctx, org); err != nil {
		ctx.Err.Printf("/dashboard/orgs/%s profile: %s", organizationID, err)
		http.Redirect(w, r, destination+"?error="+url.QueryEscape("Organization profile could not be saved."), http.StatusSeeOther)
		return
	}
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.profile_updated", "organization", organizationID, map[string]any{"role": membership.Role})
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape("Organization profile updated."), http.StatusSeeOther)
}

func OrganizationDashboardInviteCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, membership, organizationID, destination, ok := organizationManagerMutation(w, r, ctx)
	if !ok {
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	token, invite, err := getters.CreateOrganizationMemberInvite(ctx, organizationID, r.FormValue("email"), r.FormValue("role"), id.PersonID, time.Now().Add(72*time.Hour))
	if err != nil {
		message := "The organization invitation could not be created."
		if errors.Is(err, getters.ErrOrganizationMemberInvitePending) {
			message = "An invitation for that email is already pending. Create a new link from the pending invitations list if needed."
		}
		http.Redirect(w, r, destination+"?error="+url.QueryEscape(message), http.StatusSeeOther)
		return
	}
	ctx.Session.Put(r.Context(), organizationInviteLinkSessionKey, ctx.Env.GetURI()+"/sponsor-invites/"+url.PathEscape(token))
	ctx.Session.Put(r.Context(), organizationInviteLinkSessionKey+"_email", invite.Email)
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.member_invited", "organization_member_invite", invite.ID, map[string]any{"email": invite.Email, "role": invite.Role})
	invite.OrganizationName = membership.Organization.Name
	next := "/sponsor-invites/" + url.PathEscape(token)
	loginURL := auth.MagicLink(ctx, invite.Email, next)
	var sendErr error
	if loginURL == "" {
		sendErr = errors.New("could not create invitation login link")
	} else {
		sendErr = emails.SendOrganizationMemberInvitation(ctx, invite, loginURL)
	}
	if sendErr != nil {
		ctx.Err.Printf("/dashboard/orgs/%s invitation email to %s: %s", organizationID, invite.Email, sendErr)
		http.Redirect(w, r, destination+"?error="+url.QueryEscape("The invitation was created, but the email could not be sent. Copy the secure link below instead."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape("Invitation emailed to "+invite.Email+"."), http.StatusSeeOther)
}

func OrganizationDashboardMemberAdd(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, organizationID, destination, ok := organizationManagerMutation(w, r, ctx)
	if !ok {
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	personID := strings.TrimSpace(r.FormValue("person_id"))
	role := strings.ToLower(strings.TrimSpace(r.FormValue("role")))
	if role != getters.OrganizationRoleMember && role != getters.OrganizationRoleManager {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape("Organization teammates can be added as a member or manager."), http.StatusSeeOther)
		return
	}
	person, err := getters.FetchSpeakerByID(ctx, personID)
	if err != nil || person == nil {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape("Choose a person from the search results."), http.StatusSeeOther)
		return
	}
	if err := getters.AddOrganizationMembershipAsAdmin(ctx, organizationID, person.ID, role, id.PersonID); err != nil {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.member_added", "person", person.ID, map[string]any{"role": role})
	name := strings.TrimSpace(person.Name)
	if name == "" {
		name = "Teammate"
	}
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape(name+" added as "+role+"."), http.StatusSeeOther)
}

func OrganizationDashboardPersonSearch(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := auth.RequireOptional(r, ctx)
	if id == nil || strings.TrimSpace(id.PersonID) == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	membership, err := getters.GetOrganizationMembership(ctx, id.PersonID, organizationID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orgs/%s people search authorization: %s", organizationID, err)
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}
	if membership == nil || !sponsorMembershipCanManage(membership) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writePersonSearchResults(w, r, ctx)
}

func OrganizationDashboardInviteReplace(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, organizationID, destination, ok := organizationManagerMutation(w, r, ctx)
	if !ok {
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	inviteID := strings.TrimSpace(mux.Vars(r)["inviteID"])
	token, invite, err := getters.ReplaceOrganizationMemberInvite(ctx, organizationID, inviteID, id.PersonID, time.Now().Add(72*time.Hour))
	if err != nil {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	ctx.Session.Put(r.Context(), organizationInviteLinkSessionKey, ctx.Env.GetURI()+"/sponsor-invites/"+url.PathEscape(token))
	ctx.Session.Put(r.Context(), organizationInviteLinkSessionKey+"_email", invite.Email)
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.member_invite_replaced", "organization_member_invite", invite.ID, map[string]any{"email": invite.Email, "replaced_invite_id": inviteID})
	if sendErr := sendOrganizationMemberInvitationEmail(ctx, organizationID, invite, token); sendErr != nil {
		ctx.Err.Printf("/dashboard/orgs/%s replacement invitation email to %s: %s", organizationID, invite.Email, sendErr)
		http.Redirect(w, r, destination+"?error="+url.QueryEscape("The invitation was replaced, but its email could not be sent. Copy the secure link below instead."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape("A replacement invitation was emailed. The previous link is no longer valid."), http.StatusSeeOther)
}

func OrganizationDashboardInviteRevoke(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, _, organizationID, destination, ok := organizationManagerMutation(w, r, ctx)
	if !ok {
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	inviteID := strings.TrimSpace(mux.Vars(r)["inviteID"])
	if err := getters.RevokeOrganizationMemberInviteAsAdmin(ctx, organizationID, inviteID); err != nil {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.member_invite_revoked", "organization_member_invite", inviteID, nil)
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape("Pending invitation revoked."), http.StatusSeeOther)
}

func OrganizationDashboardMemberRoleUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, membership, organizationID, destination, ok := organizationManagerMutation(w, r, ctx)
	if !ok {
		return
	}
	if membership.Role != getters.OrganizationRoleOwner {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape("Only organization owners can change member roles."), http.StatusSeeOther)
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	targetPersonID := strings.TrimSpace(mux.Vars(r)["personID"])
	role := strings.ToLower(strings.TrimSpace(r.FormValue("role")))
	if err := getters.UpdateOrganizationMembershipRole(ctx, organizationID, id.PersonID, targetPersonID, role); err != nil {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.member_role_changed", "person", targetPersonID, map[string]any{"role": role})
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape("Organization member role updated."), http.StatusSeeOther)
}

func OrganizationDashboardMemberRemove(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, memberships, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	targetPersonID := strings.TrimSpace(mux.Vars(r)["personID"])
	destination := "/dashboard/orgs/" + url.PathEscape(organizationID)
	if organizationMembershipByID(memberships, organizationID) == nil {
		http.Redirect(w, r, "/dashboard/orgs?error="+url.QueryEscape("You are not a member of that organization."), http.StatusSeeOther)
		return
	}
	if !parseOrganizationDashboardForm(w, r, ctx) {
		return
	}
	if err := getters.RemoveOrganizationMembership(ctx, organizationID, id.PersonID, targetPersonID); err != nil {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	recordOrganizationDashboardAudit(ctx, organizationID, id.PersonID, "organization.member_removed", "person", targetPersonID, map[string]any{"self_removed": id.PersonID == targetPersonID})
	if id.PersonID == targetPersonID {
		http.Redirect(w, r, "/dashboard/orgs?flash="+url.QueryEscape("You left the organization."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, destination+"?flash="+url.QueryEscape("Organization member removed."), http.StatusSeeOther)
}

func organizationDashboardIdentity(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*auth.Identity, []*types.OrganizationMembership, bool) {
	id, err := auth.Resolve(r, ctx)
	if err != nil {
		ctx.Err.Printf("organization dashboard identity: %s", err)
		http.Error(w, "Unable to resolve account", http.StatusInternalServerError)
		return nil, nil, false
	}
	if id == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return nil, nil, false
	}
	if strings.TrimSpace(id.PersonID) == "" {
		return id, nil, true
	}
	memberships, err := getters.ListOrganizationMembershipsForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("organization dashboard memberships for %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load organizations", http.StatusInternalServerError)
		return nil, nil, false
	}
	return id, memberships, true
}

func organizationManagerMutation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*auth.Identity, *types.OrganizationMembership, string, string, bool) {
	id, memberships, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok {
		return nil, nil, "", "", false
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	destination := "/dashboard/orgs/" + url.PathEscape(organizationID)
	membership := organizationMembershipByID(memberships, organizationID)
	if membership == nil || !sponsorMembershipCanManage(membership) {
		http.Redirect(w, r, destination+"?error="+url.QueryEscape("Only organization owners and managers can make that change."), http.StatusSeeOther)
		return nil, nil, organizationID, destination, false
	}
	return id, membership, organizationID, destination, true
}

func parseOrganizationDashboardForm(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) bool {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form token", http.StatusBadRequest)
		return false
	}
	return true
}

func recordOrganizationDashboardAudit(ctx *config.AppContext, organizationID, actorPersonID, action, targetType, targetID string, details map[string]any) {
	if err := getters.RecordSponsorAuditEvent(ctx, organizationID, "", "", actorPersonID, action, targetType, targetID, details); err != nil {
		ctx.Err.Printf("/dashboard/orgs/%s audit %s: %s", organizationID, action, err)
	}
}
