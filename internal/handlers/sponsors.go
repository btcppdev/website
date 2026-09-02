package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

type OrgListPage struct {
	Orgs         []*types.Org
	FlashMessage string
	Year         uint
}

type OrgDetailPage struct {
	Org          *types.Org
	IsNew        bool
	FlashMessage string
	SpacesReady  bool
	Year         uint
}

type OrgNewPage struct {
	// ReturnTo is a same-site relative path the form re-submits as a
	// hidden field; OrgCreate redirects there after a successful save
	// so the admin lands back on the page they came from.
	ReturnTo     string
	FlashMessage string
	SpacesReady  bool
	Year         uint
}

type SponsorshipsPage struct {
	Conf         *types.Conf
	Sponsorships []*types.Sponsorship
	Entitlements map[string]*types.SponsorshipEntitlement
	Orgs         []*types.Org
	FlashMessage string
	FlashError   string
	Year         uint
}

func sponsorManagerInvitationFromForm(r *http.Request) (name, email string, err error) {
	name = strings.TrimSpace(r.FormValue("ManagerName"))
	email = strings.ToLower(strings.TrimSpace(r.FormValue("ManagerEmail")))
	if name == "" && email == "" {
		return "", "", nil
	}
	if name == "" || email == "" {
		return "", "", fmt.Errorf("manager name and email are both required to send an invitation")
	}
	parsed, parseErr := mail.ParseAddress(email)
	if parseErr != nil || !strings.EqualFold(parsed.Address, email) {
		return "", "", fmt.Errorf("enter a valid manager email address")
	}
	return name, email, nil
}

func sendSponsorshipManagerInvitation(ctx *config.AppContext, conf *types.Conf, org *types.Org, invitedByPersonID, name, email string) error {
	if strings.TrimSpace(email) == "" {
		return nil
	}
	token, invite, err := getters.CreateOrganizationMemberInvite(ctx, org.Ref, email, getters.OrganizationRoleManager, invitedByPersonID, time.Now().Add(72*time.Hour))
	if err != nil {
		return err
	}
	invite.OrganizationName = org.Name
	next := "/sponsor-invites/" + url.PathEscape(token)
	if strings.TrimSpace(name) != "" {
		next += "?name=" + url.QueryEscape(strings.TrimSpace(name))
	}
	loginURL := auth.MagicLink(ctx, invite.Email, next)
	if loginURL == "" {
		return fmt.Errorf("could not create the sponsor invitation login link")
	}
	if err := emails.SendSponsorManagerInvitation(ctx, conf, invite, name, loginURL); err != nil {
		return fmt.Errorf("send sponsor manager invitation: %w", err)
	}
	return nil
}

func (p *SponsorshipsPage) CanViewAllHackathonSubmissions(sponsorshipID string) bool {
	if p == nil || p.Entitlements == nil {
		return false
	}
	entitlement := p.Entitlements[sponsorshipID]
	return entitlement != nil && entitlement.AllHackathonSubmissions
}

func (p *SponsorshipsPage) TicketAllocation(sponsorshipID string) int {
	if p == nil || p.Entitlements == nil || p.Entitlements[sponsorshipID] == nil {
		return 0
	}
	return p.Entitlements[sponsorshipID].TicketAllocation
}

func (p *SponsorshipsPage) SponsorAwardLimit(sponsorshipID string) int {
	if p == nil || p.Entitlements == nil || p.Entitlements[sponsorshipID] == nil {
		return 0
	}
	return p.Entitlements[sponsorshipID].SponsorAwardLimit
}

func (p *SponsorshipsPage) CanEditOrganization(sponsorshipID string) bool {
	return p != nil && p.Entitlements != nil && p.Entitlements[sponsorshipID] != nil && p.Entitlements[sponsorshipID].CanEditOrganization
}

func sponsorLevelIncludesAllHackathonSubmissions(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "headline", "hackathon":
		return true
	default:
		return false
	}
}

func sponsorAllHackathonSubmissionsFromForm(r *http.Request, level string) bool {
	return sponsorLevelIncludesAllHackathonSubmissions(level) || r.FormValue("HackathonSubmissionAccess") == "all"
}

func sponsorTicketAllocationFromForm(r *http.Request) (int, error) {
	return sponsorNonNegativeCountFromForm(r, "TicketAllocation", "ticket allocation")
}

func sponsorAwardLimitFromForm(r *http.Request) (int, error) {
	return sponsorNonNegativeCountFromForm(r, "SponsorAwardLimit", "sponsor prize proposal allowance")
}

func sponsorNonNegativeCountFromForm(r *http.Request, field, label string) (int, error) {
	raw := strings.TrimSpace(r.FormValue(field))
	if raw == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("%s must be a non-negative whole number", label)
	}
	return count, nil
}

func SponsorshipPersonSearch(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	writePersonSearchResults(w, r, ctx)
}

func OrgList(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	orgs, err := getters.ListOrgs(ctx)
	if err != nil {
		http.Error(w, "Unable to load orgs", http.StatusInternalServerError)
		ctx.Err.Printf("/admin/orgs failed: %s", err.Error())
		return
	}

	sort.SliceStable(orgs, func(i, j int) bool {
		return orgs[i].Name < orgs[j].Name
	})

	err = ctx.TemplateCache.ExecuteTemplate(w, "sponsors/orgs.tmpl", &OrgListPage{
		Orgs:         orgs,
		FlashMessage: r.URL.Query().Get("flash"),
		Year:         helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/admin/orgs template failed: %s", err.Error())
	}
}

func OrgDetail(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	params := mux.Vars(r)
	ref := params["ref"]

	if ref == "new" {
		err := ctx.TemplateCache.ExecuteTemplate(w, "sponsors/detail.tmpl", &OrgDetailPage{
			Org:         &types.Org{},
			IsNew:       true,
			SpacesReady: spaces.IsConfigured(),
			Year:        helpers.CurrentYear(),
		})
		if err != nil {
			http.Error(w, "Unable to load page", http.StatusInternalServerError)
			ctx.Err.Printf("/admin/orgs/new template failed: %s", err.Error())
		}
		return
	}

	org, err := getters.GetOrg(ctx, ref)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	err = ctx.TemplateCache.ExecuteTemplate(w, "sponsors/detail.tmpl", &OrgDetailPage{
		Org:          org,
		FlashMessage: r.URL.Query().Get("flash"),
		SpacesReady:  spaces.IsConfigured(),
		Year:         helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/admin/orgs/%s template failed: %s", ref, err.Error())
	}
}

// OrgNew renders the GET form for creating a new Org. Optional `return`
// query param (caller-supplied URL, must be relative to the site) tells
// OrgCreate where to redirect after a successful create — we round-trip
// it as a hidden form field so the POST handler can consume it.
func OrgNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	page := &OrgNewPage{
		ReturnTo:     safeReturnTo(r.URL.Query().Get("return")),
		FlashMessage: r.URL.Query().Get("flash"),
		SpacesReady:  spaces.IsConfigured(),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "sponsors/org_new.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/orgs/new render: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

// OrgLogoUpload accepts a multipart `file` upload from the org form's
// inline "Upload a file" affordance, mirrors it to Spaces under
// sponsors/{shortID}{ext}, and returns the public URL as JSON
// `{url: "..."}` so the page JS can drop it into the URL input.
//
// Idempotent on identical file content via the shortID +
// spaces.Exists short-circuit, mirroring mirrorOrgLogoToSpaces.
// Gated to global-admin since /admin/orgs/* is.
func OrgLogoUpload(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	limitRequestBody(w, r, maxMultipartBodyBytes)
	raw, contentType, ext, err := readMultipartLogoFile(r, "file")
	if err != nil {
		http.Error(w, "missing or unreadable file", http.StatusBadRequest)
		return
	}
	if !spaces.IsConfigured() {
		http.Error(w, "spaces not configured", http.StatusInternalServerError)
		return
	}
	shortID := imgproc.ShortID(raw)
	key := "sponsors/" + shortID + ext
	if !spaces.Exists(key) {
		if _, err := spaces.Upload(key, raw, contentType, ""); err != nil {
			ctx.Err.Printf("/admin/orgs/upload-logo: %s", err)
			http.Error(w, "upload failed", http.StatusInternalServerError)
			return
		}
	}
	newPhotoPipeline(ctx).updateOrgLogoManifest(key, raw)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": spaces.PublicURL(key)})
}

func OrgCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	org := &types.Org{
		Name:      strings.TrimSpace(r.FormValue("Name")),
		Tagline:   strings.TrimSpace(r.FormValue("Tagline")),
		Email:     strings.TrimSpace(r.FormValue("Email")),
		Website:   strings.TrimSpace(r.FormValue("Website")),
		Twitter:   types.ParseTwitter(r.FormValue("Twitter")),
		Nostr:     strings.TrimSpace(r.FormValue("Nostr")),
		Matrix:    strings.TrimSpace(r.FormValue("Matrix")),
		LinkedIn:  strings.TrimSpace(r.FormValue("LinkedIn")),
		Instagram: strings.TrimSpace(r.FormValue("Instagram")),
		Youtube:   strings.TrimSpace(r.FormValue("Youtube")),
		Github:    strings.TrimSpace(r.FormValue("Github")),
		LogoLight: strings.TrimSpace(r.FormValue("LogoLight")),
		LogoDark:  strings.TrimSpace(r.FormValue("LogoDark")),
		Hiring:    r.FormValue("Hiring") == "on",
		Notes:     strings.TrimSpace(r.FormValue("Notes")),
	}
	trimOrg(org)

	if org.Name == "" {
		http.Error(w, "Org name is required", http.StatusBadRequest)
		return
	}

	_, err := getters.RegisterOrg(ctx, org)
	if err != nil {
		ctx.Err.Printf("/admin/orgs/new failed: %s", err.Error())
		http.Error(w, "Failed to create org", http.StatusInternalServerError)
		return
	}

	dest := safeReturnTo(r.FormValue("return"))
	if dest == "" {
		dest = "/admin/orgs"
	}
	dest = appendFlash(dest, "Org "+org.Name+" created")
	http.Redirect(w, r, dest, http.StatusFound)
}

func OrgSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	ref := strings.TrimSpace(mux.Vars(r)["ref"])
	if ref == "" {
		handle404(w, r, ctx)
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	org := &types.Org{
		Ref:       ref,
		Name:      strings.TrimSpace(r.FormValue("Name")),
		Tagline:   strings.TrimSpace(r.FormValue("Tagline")),
		Email:     strings.TrimSpace(r.FormValue("Email")),
		Website:   strings.TrimSpace(r.FormValue("Website")),
		Twitter:   types.ParseTwitter(r.FormValue("Twitter")),
		Nostr:     strings.TrimSpace(r.FormValue("Nostr")),
		Matrix:    strings.TrimSpace(r.FormValue("Matrix")),
		LinkedIn:  strings.TrimSpace(r.FormValue("LinkedIn")),
		Instagram: strings.TrimSpace(r.FormValue("Instagram")),
		Youtube:   strings.TrimSpace(r.FormValue("Youtube")),
		Github:    strings.TrimSpace(r.FormValue("Github")),
		LogoLight: strings.TrimSpace(r.FormValue("LogoLight")),
		LogoDark:  strings.TrimSpace(r.FormValue("LogoDark")),
		Hiring:    r.FormValue("Hiring") == "on",
		Notes:     strings.TrimSpace(r.FormValue("Notes")),
	}
	trimOrg(org)

	if org.Name == "" {
		http.Error(w, "Org name is required", http.StatusBadRequest)
		return
	}

	if err := getters.UpdateOrgDetails(ctx, org); err != nil {
		ctx.Err.Printf("/admin/orgs/%s save failed: %s", ref, err)
		http.Error(w, "Failed to save org", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/orgs/"+url.PathEscape(ref)+"?flash="+url.QueryEscape("Org "+org.Name+" saved"), http.StatusFound)
}

// safeReturnTo accepts only same-site relative paths so the redirect
// can't be hijacked into an open-redirect against another origin.
func safeReturnTo(raw string) string {
	if raw == "" {
		return ""
	}
	// Must start with / and not //, must not contain a scheme.
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return ""
	}
	if strings.Contains(raw, ":") {
		return ""
	}
	return raw
}

// appendFlash adds a ?flash=… param to a URL, preserving any existing
// query string. Used so the redirect target's flash banner picks up.
func appendFlash(rawURL, msg string) string {
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + "flash=" + url.QueryEscape(msg)
}

func SponsorshipsList(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	sponsorships, err := getters.ListSponsorships(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load sponsorships", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/sponsors failed: %s", conf.Tag, err.Error())
		return
	}
	entitlements, err := getters.ListSponsorshipEntitlementsForConference(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load sponsorship benefits", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/sponsors failed to load entitlements: %s", conf.Tag, err.Error())
		return
	}

	orgs, err := getters.ListOrgs(ctx)
	if err != nil {
		http.Error(w, "Unable to load orgs", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/sponsors failed to load orgs: %s", conf.Tag, err.Error())
		return
	}

	sort.SliceStable(orgs, func(i, j int) bool {
		return orgs[i].Name < orgs[j].Name
	})

	err = ctx.TemplateCache.ExecuteTemplate(w, "sponsors/events.tmpl", &SponsorshipsPage{
		Conf:         conf,
		Sponsorships: sponsorships,
		Entitlements: entitlements,
		Orgs:         orgs,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/sponsors template failed: %s", conf.Tag, err.Error())
	}
}

func SponsorshipCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireConfAdmin(w, r, ctx)
	if id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	orgRef := strings.TrimSpace(r.FormValue("OrgRef"))
	level := strings.TrimSpace(r.FormValue("Level"))
	ticketAllocation, allocationErr := sponsorTicketAllocationFromForm(r)
	if allocationErr != nil {
		http.Error(w, allocationErr.Error(), http.StatusBadRequest)
		return
	}
	sponsorAwardLimit, awardLimitErr := sponsorAwardLimitFromForm(r)
	if awardLimitErr != nil {
		http.Error(w, awardLimitErr.Error(), http.StatusBadRequest)
		return
	}
	managerPersonID := strings.TrimSpace(r.FormValue("ManagerPersonID"))
	managerName, managerEmail, managerInviteErr := sponsorManagerInvitationFromForm(r)
	if managerInviteErr != nil {
		http.Error(w, managerInviteErr.Error(), http.StatusBadRequest)
		return
	}
	if managerPersonID != "" && managerEmail != "" {
		http.Error(w, "choose an existing manager or invite a new manager, not both", http.StatusBadRequest)
		return
	}
	if managerPersonID != "" {
		if err := validatePersonIDs(ctx, []string{managerPersonID}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if orgRef == "" || level == "" {
		http.Error(w, "Org and level are required", http.StatusBadRequest)
		return
	}

	org, err := getters.GetOrg(ctx, orgRef)
	if err != nil || org == nil {
		http.Error(w, "Org not found", http.StatusBadRequest)
		return
	}

	sp := &types.Sponsorship{
		Org:    org,
		Confs:  []*types.Conf{conf},
		Level:  level,
		Status: "Pending",
	}

	entitlement := &types.SponsorshipEntitlement{
		ConferenceID:                     conf.Ref,
		TicketAllocation:                 ticketAllocation,
		SponsorAwardLimit:                sponsorAwardLimit,
		AllHackathonSubmissions:          sponsorAllHackathonSubmissionsFromForm(r, level),
		AutomaticSubmissionContactAccess: sponsorLevelIncludesAllHackathonSubmissions(level),
		ParticipantContactExport:         sponsorLevelIncludesAllHackathonSubmissions(level),
		CanEditOrganization:              r.FormValue("CanEditOrganization") == "on",
	}
	err = getters.RegisterSponsorship(ctx, sp, entitlement, getters.SponsorshipWriteOptions{
		ManagerPersonID: managerPersonID, AssignedByPersonID: id.PersonID,
	})
	if err != nil {
		ctx.Err.Printf("/%s/admin/sponsors/new failed: %s", conf.Tag, err.Error())
		http.Error(w, "Failed to create sponsorship", http.StatusInternalServerError)
		return
	}

	dest := "/" + conf.Tag + "/admin/sponsors"
	flash := "Sponsorship created."
	if managerPersonID != "" {
		flash = "Sponsorship created and organization manager added."
	}
	if managerEmail != "" {
		if err := sendSponsorshipManagerInvitation(ctx, conf, org, id.PersonID, managerName, managerEmail); err != nil {
			ctx.Err.Printf("/%s/admin/sponsors/new manager invitation: %s", conf.Tag, err)
			http.Redirect(w, r, dest+"?flash="+url.QueryEscape(flash)+"&error="+url.QueryEscape("The sponsorship was saved, but the manager invitation could not be sent: "+err.Error()), http.StatusSeeOther)
			return
		}
		flash = "Sponsorship created and manager invitation emailed to " + managerEmail + "."
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

func SponsorshipUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireConfAdmin(w, r, ctx)
	if id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	ref := strings.TrimSpace(mux.Vars(r)["ref"])
	if ref == "" {
		handle404(w, r, ctx)
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	orgRef := strings.TrimSpace(r.FormValue("OrgRef"))
	level := strings.TrimSpace(r.FormValue("Level"))
	status := strings.TrimSpace(r.FormValue("Status"))
	ticketAllocation, allocationErr := sponsorTicketAllocationFromForm(r)
	if allocationErr != nil {
		http.Error(w, allocationErr.Error(), http.StatusBadRequest)
		return
	}
	sponsorAwardLimit, awardLimitErr := sponsorAwardLimitFromForm(r)
	if awardLimitErr != nil {
		http.Error(w, awardLimitErr.Error(), http.StatusBadRequest)
		return
	}
	managerPersonID := strings.TrimSpace(r.FormValue("ManagerPersonID"))
	managerName, managerEmail, managerInviteErr := sponsorManagerInvitationFromForm(r)
	if managerInviteErr != nil {
		http.Error(w, managerInviteErr.Error(), http.StatusBadRequest)
		return
	}
	if managerPersonID != "" && managerEmail != "" {
		http.Error(w, "choose an existing manager or invite a new manager, not both", http.StatusBadRequest)
		return
	}
	if managerPersonID != "" {
		if err := validatePersonIDs(ctx, []string{managerPersonID}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if orgRef == "" || level == "" {
		http.Error(w, "Org and level are required", http.StatusBadRequest)
		return
	}
	if status == "" {
		status = "Pending"
	}

	org, err := getters.GetOrg(ctx, orgRef)
	if err != nil {
		http.Error(w, "Org not found", http.StatusBadRequest)
		return
	}

	sp := &types.Sponsorship{
		Ref:      ref,
		Org:      org,
		Level:    level,
		Label:    strings.TrimSpace(r.FormValue("Label")),
		Status:   status,
		IsVendor: r.FormValue("IsVendor") == "on",
		Notes:    strings.TrimSpace(r.FormValue("Notes")),
	}
	entitlement := &types.SponsorshipEntitlement{
		SponsorshipID:                    ref,
		ConferenceID:                     conf.Ref,
		TicketAllocation:                 ticketAllocation,
		SponsorAwardLimit:                sponsorAwardLimit,
		AllHackathonSubmissions:          sponsorAllHackathonSubmissionsFromForm(r, level),
		AutomaticSubmissionContactAccess: sponsorLevelIncludesAllHackathonSubmissions(level),
		ParticipantContactExport:         sponsorLevelIncludesAllHackathonSubmissions(level),
		CanEditOrganization:              r.FormValue("CanEditOrganization") == "on",
	}
	if err := getters.UpdateSponsorship(ctx, conf.Ref, sp, entitlement, getters.SponsorshipWriteOptions{
		ManagerPersonID: managerPersonID, AssignedByPersonID: id.PersonID,
	}); err != nil {
		ctx.Err.Printf("/%s/admin/sponsors/%s update failed: %s", conf.Tag, ref, err.Error())
		http.Error(w, "Failed to update sponsorship", http.StatusInternalServerError)
		return
	}

	dest := "/" + conf.Tag + "/admin/sponsors"
	flash := "Sponsorship updated."
	if managerPersonID != "" {
		flash = "Sponsorship updated and organization manager added."
	}
	if managerEmail != "" {
		if err := sendSponsorshipManagerInvitation(ctx, conf, org, id.PersonID, managerName, managerEmail); err != nil {
			ctx.Err.Printf("/%s/admin/sponsors/%s manager invitation: %s", conf.Tag, ref, err)
			http.Redirect(w, r, dest+"?flash="+url.QueryEscape(flash)+"&error="+url.QueryEscape("The sponsorship was saved, but the manager invitation could not be sent: "+err.Error()), http.StatusSeeOther)
			return
		}
		flash = "Sponsorship updated and manager invitation emailed to " + managerEmail + "."
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

func SponsorshipDelete(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	ref := strings.TrimSpace(mux.Vars(r)["ref"])
	if ref == "" {
		handle404(w, r, ctx)
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	if err := getters.DeleteSponsorship(ctx, conf.Ref, ref); err != nil {
		ctx.Err.Printf("/%s/admin/sponsors/%s delete failed: %s", conf.Tag, ref, err.Error())
		http.Error(w, "Failed to remove sponsorship", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/"+conf.Tag+"/admin/sponsors"+"?flash=Sponsorship+removed", http.StatusFound)
}
