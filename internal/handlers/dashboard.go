package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	appmetrics "btcpp-web/internal/observability"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

// Dashboard is the session-authenticated self-service page combining a
// speaker's talk applications + their volunteer shift signups. Magic links
// establish a session at /auth before redirecting here.
func Dashboard(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if r.Method == http.MethodPost {
		redirectDashboardLogin(w, r, "")
		return
	}

	identityStart := time.Now()
	email, resolvedIdentity, err := validatedSessionIdentity(r, ctx, true)
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "identity", time.Since(identityStart).Seconds())
	encodedHMAC := ""
	encodedEmail := ""
	if err != nil {
		ctx.Infos.Printf("/dashboard session validation failed: %s", err)
		redirectDashboardLogin(w, r, r.URL.Query().Get("error"))
		return
	}
	personID := ""
	if resolvedIdentity != nil {
		personID = resolvedIdentity.PersonID
	}

	dashStart := time.Now()
	reqID := requestID(r)
	defer func() {
		ctx.Infos.Printf("/dashboard id=%s total=%s", reqID, time.Since(dashStart))
	}()

	// Top-level fan-out: speakerconfs + volunteer apps + user's tickets
	// are all independent.
	var (
		speakers             []*types.Speaker
		speakerConfs         []*types.SpeakerConf
		scErr                error
		volapps              []*types.Volunteer
		volErr               error
		regs                 []*types.Registration
		regErr               error
		satEvents            []*types.SatelliteEvent
		satErr               error
		judgeAssignments     []*types.CompetitionJudgeAssignment
		judgeErr             error
		hasHackathonProjects bool
		projectsErr          error
		shopOrders           []*types.ShopOrder
		shopErr              error
		hasSponsorMembership bool
		sponsorErr           error
	)
	t1 := time.Now()
	var topWg sync.WaitGroup
	topWg.Add(8)
	var scDur, volDur, regDur, satDur, judgeDur, projectsDur, shopDur, sponsorDur time.Duration
	go func() {
		defer topWg.Done()
		s := time.Now()
		if personID != "" && resolvedIdentity != nil && resolvedIdentity.Speaker != nil {
			speakers = []*types.Speaker{resolvedIdentity.Speaker}
			speakerConfs, scErr = getters.ListSpeakerConfsForSpeaker(ctx, resolvedIdentity.Speaker)
		} else if personID != "" {
			speakers, speakerConfs, scErr = getters.GetSpeakerConfsByPersonID(ctx, personID)
		} else {
			speakers, speakerConfs, scErr = getters.GetSpeakerConfsByEmail(ctx, email)
		}
		scDur = time.Since(s)
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "speaker_graph", scDur.Seconds())
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		if personID != "" {
			volapps, volErr = getters.ListVolunteerAppsForPerson(ctx, personID)
		} else {
			volapps, volErr = getters.ListVolunteerApps(ctx, email)
		}
		volDur = time.Since(s)
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "volunteer_apps", volDur.Seconds())
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		if personID != "" {
			regs, regErr = getters.ListRegistrationsForPerson(ctx, personID)
		} else {
			regs, regErr = getters.ListRegistrationsByEmail(ctx, email)
		}
		regDur = time.Since(s)
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "registrations", regDur.Seconds())
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		if personID != "" {
			satEvents, satErr = getters.ListSatelliteEventsForPerson(ctx, personID)
		} else {
			satEvents, satErr = getters.ListSatelliteEventsBySubmitter(ctx, email)
		}
		satDur = time.Since(s)
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "satellite_events", satDur.Seconds())
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		if personID != "" {
			judgeAssignments, judgeErr = getters.ListCompetitionJudgeAssignmentsForPerson(ctx, personID)
			if judgeErr == nil {
				var sponsorAssignments []*types.CompetitionJudgeAssignment
				sponsorAssignments, judgeErr = getters.ListAwardJudgeAssignmentsForPerson(ctx, personID)
				judgeAssignments = append(judgeAssignments, sponsorAssignments...)
			}
		} else {
			judgeAssignments, judgeErr = getters.ListCompetitionJudgeAssignmentsByEmail(ctx, email)
			if judgeErr == nil {
				var sponsorAssignments []*types.CompetitionJudgeAssignment
				sponsorAssignments, judgeErr = getters.ListAwardJudgeAssignmentsByEmail(ctx, email)
				judgeAssignments = append(judgeAssignments, sponsorAssignments...)
			}
		}
		judgeDur = time.Since(s)
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "judging", judgeDur.Seconds())
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		if personID != "" {
			hasHackathonProjects, projectsErr = getters.HasHackathonParticipantProjectsForPerson(ctx, personID)
		}
		projectsDur = time.Since(s)
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "hackathon_projects", projectsDur.Seconds())
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		if personID != "" {
			shopOrders, shopErr = getters.ListShopOrdersForPerson(ctx, personID, 5)
		} else {
			shopOrders, shopErr = getters.ListShopOrdersByEmail(ctx, email, 5)
		}
		shopDur = time.Since(s)
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "shop_orders", shopDur.Seconds())
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		if personID != "" {
			hasSponsorMembership, sponsorErr = getters.HasActiveOrganizationMembership(ctx, personID)
		}
		sponsorDur = time.Since(s)
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "sponsor_membership", sponsorDur.Seconds())
	}()
	topWg.Wait()
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "account_fetch", time.Since(t1).Seconds())
	ctx.Infos.Printf("/dashboard id=%s fetch wall=%s (sc=%s vol=%s reg=%s sat=%s judge=%s projects=%s shop=%s sponsor=%s) → speakers=%d speakerConfs=%d volapps=%d regs=%d satellites=%d judgeAssignments=%d hasProjects=%t shopOrders=%d hasSponsorOrg=%t",
		reqID, time.Since(t1), scDur, volDur, regDur, satDur, judgeDur, projectsDur, shopDur, sponsorDur, len(speakers), len(speakerConfs), len(volapps), len(regs), len(satEvents), len(judgeAssignments), hasHackathonProjects, len(shopOrders), hasSponsorMembership)
	if regErr != nil {
		ctx.Err.Printf("/dashboard listregs failed (continuing): %s", regErr)
	}
	if satErr != nil {
		ctx.Err.Printf("/dashboard satellite events failed (continuing): %s", satErr)
	}
	if judgeErr != nil {
		ctx.Err.Printf("/dashboard judge assignments failed (continuing): %s", judgeErr)
	}
	if projectsErr != nil {
		ctx.Err.Printf("/dashboard participant projects failed (continuing): %s", projectsErr)
		hasHackathonProjects = false
	}
	if shopErr != nil {
		ctx.Err.Printf("/dashboard shop orders failed (continuing): %s", shopErr)
		shopOrders = nil
	}
	if sponsorErr != nil {
		ctx.Err.Printf("/dashboard sponsor memberships failed (continuing): %s", sponsorErr)
		hasSponsorMembership = false
	}
	// Drop revoked tickets and sponsored-builder purchases. Both stay
	// in the database for staff reporting, but neither is an attendee
	// admission QR for the buyer dashboard.
	if len(regs) > 0 {
		live := regs[:0]
		for _, r := range regs {
			if r != nil && !r.Revoked && !types.IsSponsoredTicketType(r.Type) {
				live = append(live, r)
			}
		}
		regs = live
	}
	qrStart := time.Now()
	attachRegistrationQRCodes(ctx, regs)
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "ticket_qr", time.Since(qrStart).Seconds())

	if scErr != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/dashboard speakerconfs failed: %s", scErr)
		return
	}
	if volErr != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/dashboard listvolunteerapps failed: %s", volErr)
		return
	}

	// Volunteer-side: VolInfo + per-vol shifts can all run in parallel.
	var volInfosByConf map[string]*types.VolInfo
	if len(volapps) > 0 {
		t2 := time.Now()
		var volInfoErr error
		var volInfoDur time.Duration
		var volWg sync.WaitGroup
		volWg.Add(1)
		go func() {
			defer volWg.Done()
			s := time.Now()
			volInfosByConf, volInfoErr = getters.GetVolInfoMap(ctx)
			volInfoDur = time.Since(s)
		}()
		shiftDurs := make([]time.Duration, len(volapps))
		for i, vol := range volapps {
			if len(vol.ScheduleFor) == 0 {
				continue
			}
			volWg.Add(1)
			go func(i int, vol *types.Volunteer) {
				defer volWg.Done()
				s := time.Now()
				confTag := vol.ScheduleFor[0].Tag
				confShifts, err := getters.GetShiftsForConf(ctx, confTag)
				if err != nil {
					ctx.Err.Printf("/dashboard get shifts for %s failed: %s", confTag, err)
					return
				}
				vol.WorkShifts = getSelectedShifts(vol, confShifts)
				shiftDurs[i] = time.Since(s)
			}(i, vol)
		}
		volWg.Wait()
		appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "volunteer_enrichment", time.Since(t2).Seconds())
		var maxShift time.Duration
		for _, d := range shiftDurs {
			if d > maxShift {
				maxShift = d
			}
		}
		ctx.Infos.Printf("/dashboard id=%s fetch-vol wall=%s (volinfo=%s slowest-shift=%s of %d)",
			reqID, time.Since(t2), volInfoDur, maxShift, len(volapps))
		if volInfoErr != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/dashboard getvolinfomap failed: %s", volInfoErr)
			return
		}
	}

	name, hometown := dashboardIdentity(speakers, speakerConfs, volapps)
	var photo string
	if len(speakers) > 0 {
		photo = speakers[0].Photo
	}
	stats := calcDashboardStats(speakerConfs, volapps)

	tConfs := time.Now()
	confs := listConfs(w, ctx)
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "conference_fetch", time.Since(tConfs).Seconds())
	ctx.Infos.Printf("/dashboard id=%s listConfs=%s", reqID, time.Since(tConfs))

	t3 := time.Now()
	enrichDashboardProposals(ctx, speakerConfs)
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "proposal_enrichment", time.Since(t3).Seconds())
	ctx.Infos.Printf("/dashboard id=%s enrich-proposals=%s", reqID, time.Since(t3))

	activeSC, pastSC := splitSpeakerConfsByEnded(speakerConfs)
	activeVol, pastVol := splitVolAppsByEnded(volapps)
	ctx.Infos.Printf("/dashboard id=%s split activeSC=%d pastSC=%d activeVol=%d pastVol=%d",
		reqID, len(activeSC), len(pastSC), len(activeVol), len(pastVol))
	eligible := eligibleApplyConfs(confs, speakerConfs)
	buyable := buyableConfs(confs)
	// Award ticket entitlements may be claimed for every upcoming active
	// conference, even when that conference already has a dashboard card.
	ticketClaimConfs := append([]*types.Conf(nil), buyable...)
	tickets := upcomingTickets(regs, confs)

	activeBlocks, pastBlocks := buildEventBlocks(speakerConfs, volapps, tickets, regs, confs, volInfosByConf)
	activeBlocks, pastBlocks = attachSatelliteEventBlocks(activeBlocks, pastBlocks, satEvents, confs)
	activeBlocks, pastBlocks = attachJudgeEventBlocks(activeBlocks, pastBlocks, judgeAssignments, confs)
	// Discover sections at the bottom of the page list confs the user
	// has *no* existing relationship with. Anything already showing as
	// an event block is filtered out so we don't list it twice.
	eligible = excludeConfsInBlocks(eligible, activeBlocks)
	buyable = excludeConfsInBlocks(buyable, activeBlocks)
	ctx.Infos.Printf("/dashboard id=%s blocks active=%d past=%d eligible=%d buyable=%d",
		reqID, len(activeBlocks), len(pastBlocks), len(eligible), len(buyable))

	dashboardEnrichmentStart := time.Now()
	var topSpeaker *types.Speaker
	if len(speakers) > 0 {
		topSpeaker = speakers[0]
	}
	entitlementStart := time.Now()
	ticketEntitlements, entitlementErr := dashboardTicketEntitlements(ctx, speakers)
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "ticket_entitlements", time.Since(entitlementStart).Seconds())
	if entitlementErr != nil {
		ctx.Err.Printf("/dashboard ticket entitlements: %s", entitlementErr)
	}
	unclaimedTicketEntitlements := 0
	for _, entitlement := range ticketEntitlements {
		if entitlement != nil && entitlement.ClaimedAt == nil && entitlement.VoidedAt == nil {
			unclaimedTicketEntitlements++
		}
	}
	profileStart := time.Now()
	publicProfileID, hasPublicProfile := dashboardCachedWhoIsPublicID(ctx, topSpeaker)
	archiveOwnerPath := ""
	if hasPublicProfile {
		archiveOwnerPath = "/whois/" + url.PathEscape(publicProfileID) + "/archive"
	}
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "profile_lookup", time.Since(profileStart).Seconds())
	id := resolvedIdentity
	if id == nil {
		id = &auth.Identity{Email: email}
	}
	attachDashboardAdminRoles(activeBlocks, id)
	attachDashboardAdminRoles(pastBlocks, id)
	activeBlocks, pastBlocks = attachDashboardHackathonManagerRoles(activeBlocks, pastBlocks, confs, id)
	// Synthesize event blocks for confs the user can admin but has no
	// other relationship with (the global-admin case, or per-conf
	// admins watching events they're not personally speaking at).
	// Without this, an admin's dashboard would surface no Admin
	// button at all because activeBlocks is built only from
	// SpeakerConf/VolApp/Ticket relationships.
	if len(id.Roles) > 0 {
		existing := make(map[string]bool, len(activeBlocks))
		for _, b := range activeBlocks {
			if b != nil && b.Conf != nil {
				existing[b.Conf.Tag] = true
			}
		}
		for _, c := range confs {
			if c == nil || existing[c.Tag] || !c.Active {
				continue
			}
			var role string
			switch {
			case id.HasRoleForConf(c.Tag, auth.RoleAdmin):
				role = auth.RoleAdmin
			case id.HasRoleForConf(c.Tag, auth.RoleVolcoord):
				role = auth.RoleVolcoord
			default:
				continue
			}
			activeBlocks = append(activeBlocks, &EventBlock{
				Conf:      c,
				CanBuy:    c.Active && c.InFuture(),
				AdminRole: role,
			})
		}
		// Discover cards filter against activeBlocks via
		// excludeConfsInBlocks above, so re-run it now that we've
		// added admin-only blocks — a global-admin shouldn't see
		// every conf doubled (once as admin block, once as
		// "discover").
		eligible = excludeConfsInBlocks(eligible, activeBlocks)
		buyable = excludeConfsInBlocks(buyable, activeBlocks)
		// buildEventBlocks already sorted active by StartDate, but
		// the admin-only blocks just got appended to the end —
		// re-sort so the dashboard renders strictly in time order
		// regardless of which path each block came from.
		sort.Slice(activeBlocks, func(i, j int) bool {
			return activeBlocks[i].Conf.StartDate.Before(activeBlocks[j].Conf.StartDate)
		})
	}
	var hasUpTalk, hasUpVol bool
	for _, b := range activeBlocks {
		if b == nil {
			continue
		}
		if b.SpeakerConf != nil {
			hasUpTalk = true
		}
		if b.VolApp != nil {
			hasUpVol = true
		}
	}

	// Populate per-conf countdown bounds for the event-card widget.
	// Single database query (empty tag = all rows), bucket by tag→day,
	// then shallow-copy each block's Conf so the cached pointer
	// shared with other readers stays untouched.
	countdownStart := time.Now()
	infosByTag := map[string]map[int]*types.ConfInfo{}
	if cis, err := getters.ListConfInfos(ctx, ""); err != nil {
		ctx.Err.Printf("/dashboard ListConfInfos for countdown (continuing): %s", err)
	} else {
		for _, ci := range cis {
			if ci == nil || ci.Day < 1 || ci.ConfTag == "" {
				continue
			}
			m, ok := infosByTag[ci.ConfTag]
			if !ok {
				m = map[int]*types.ConfInfo{}
				infosByTag[ci.ConfTag] = m
			}
			m[ci.Day] = ci
		}
	}
	enrichBlock := func(b *EventBlock) {
		if b == nil || b.Conf == nil {
			return
		}
		copy := *b.Conf
		copy.CountdownStart, copy.CountdownEnd = computeCountdownBounds(&copy, infosByTag[copy.Tag])
		b.Conf = &copy
	}
	for _, b := range activeBlocks {
		enrichBlock(b)
	}
	for _, b := range pastBlocks {
		enrichBlock(b)
	}
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "countdown_fetch", time.Since(countdownStart).Seconds())
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "dashboard_enrichment", time.Since(dashboardEnrichmentStart).Seconds())

	affiliateStart := time.Now()
	var affiliateCode *types.DiscountCode
	var affiliateStats *AffiliateStats
	var affiliateWg sync.WaitGroup
	affiliateWg.Add(2)
	go func() {
		defer affiliateWg.Done()
		affiliateCode = loadAffiliateCode(ctx, personID, email, len(regs) > 0)
	}()
	go func() {
		defer affiliateWg.Done()
		affiliateStats = loadAffiliateStats(ctx, personID, email, len(regs) > 0)
	}()
	affiliateWg.Wait()
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "affiliate_fetch", time.Since(affiliateStart).Seconds())

	templateStart := time.Now()
	err = ctx.TemplateCache.ExecuteTemplate(w, "dashboard.tmpl", &DashboardPage{
		Name:                        name,
		Hometown:                    hometown,
		Photo:                       photo,
		Email:                       encodedEmail,
		HMAC:                        encodedHMAC,
		Speaker:                     topSpeaker,
		HasPublicProfile:            hasPublicProfile,
		ArchiveOwnerPath:            archiveOwnerPath,
		SpeakerConfs:                activeSC,
		PastSpeakerConfs:            pastSC,
		VolApps:                     activeVol,
		PastVolApps:                 pastVol,
		VolInfos:                    volInfosByConf,
		Stats:                       stats,
		Confs:                       confs,
		EligibleConfs:               eligible,
		BuyableConfs:                buyable,
		DiscoverConfs:               discoverConfs(confs, activeBlocks),
		Tickets:                     tickets,
		ActiveBlocks:                activeBlocks,
		PastBlocks:                  pastBlocks,
		HasUpcomingTalk:             hasUpTalk,
		HasUpcomingVol:              hasUpVol,
		HasHackathonProjects:        hasHackathonProjects,
		HasSponsorOrganizations:     hasSponsorMembership,
		FlashMessage:                r.URL.Query().Get("flash"),
		FlashError:                  r.URL.Query().Get("error"),
		IsGlobalAdmin:               id.IsGlobalAdmin(),
		HasAnyTicket:                len(regs) > 0,
		AffiliateCode:               affiliateCode,
		AffiliateStats:              affiliateStats,
		TicketEntitlements:          ticketEntitlements,
		UnclaimedTicketEntitlements: unclaimedTicketEntitlements,
		TicketClaimConfs:            ticketClaimConfs,
		RecentShopOrders:            shopOrders,
		BaseURI:                     ctx.Env.GetURI(),
		Year:                        helpers.CurrentYear(),
	})
	appmetrics.ObserveHandlerPhase(r.Context(), "/dashboard", "template", time.Since(templateStart).Seconds())
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/dashboard ExecuteTemplate failed: %s", err)
		return
	}
	ctx.Infos.Printf("/dashboard id=%s render=%s", reqID, time.Since(templateStart))
}

// dashboardCachedWhoIsPublicID keeps the public-directory refresh out of the
// dashboard critical path. The directory itself still refreshes synchronously
// on /whois, while dashboard requests use the last good snapshot and trigger a
// single background refresh when that snapshot is stale or absent.
func dashboardCachedWhoIsPublicID(ctx *config.AppContext, speaker *types.Speaker) (string, bool) {
	if ctx == nil || speaker == nil || strings.TrimSpace(speaker.ID) == "" {
		return "", false
	}
	whoIsCache.Lock()
	publicID := ""
	if whoIsCache.app == ctx && whoIsCache.publicIDs != nil {
		publicID = whoIsCache.publicIDs[speaker.ID]
	}
	needsRefresh := whoIsCache.app != ctx || whoIsCache.publicIDs == nil || time.Now().After(whoIsCache.expires)
	startRefresh := needsRefresh && !whoIsCache.refreshing
	if startRefresh {
		whoIsCache.refreshing = true
	}
	whoIsCache.Unlock()

	if startRefresh {
		go func() {
			_, err := buildWhoIsDirectory(ctx)
			whoIsCache.Lock()
			whoIsCache.refreshing = false
			whoIsCache.Unlock()
			if err != nil && ctx.Err != nil {
				ctx.Err.Printf("/dashboard background whois refresh: %s", err)
			}
		}()
	}
	return publicID, publicID != ""
}

func dashboardIdentityEmail(identity *auth.Identity) string {
	if identity == nil {
		return ""
	}
	if primary := strings.TrimSpace(identity.PrimaryEmail); primary != "" {
		return primary
	}
	return strings.TrimSpace(identity.LoginEmail)
}

func DashboardHackathons(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, encodedEmail, encodedHMAC, ok := dashboardRequestIdentity(w, r, ctx)
	if !ok {
		return
	}
	id, resolveErr := auth.Resolve(r, ctx)
	if resolveErr != nil {
		ctx.Err.Printf("/dashboard/hackathons identity for %s: %s", email, resolveErr)
		http.Error(w, "Unable to resolve account", http.StatusInternalServerError)
		return
	}
	if id == nil || strings.TrimSpace(id.PersonID) == "" {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("A person profile is required to view hackathon projects."), http.StatusSeeOther)
		return
	}
	participantProjects, err := getters.ListHackathonParticipantProjectsForPerson(ctx, id.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/hackathons for person %s: %s", id.PersonID, err)
		http.Error(w, "Unable to load hackathon projects", http.StatusInternalServerError)
		return
	}
	if len(participantProjects) == 0 {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("You do not have any hackathon projects yet."), http.StatusSeeOther)
		return
	}
	name := email
	photo := ""
	isGlobalAdmin := id.IsGlobalAdmin()
	hasSponsorMembership, sponsorErr := getters.HasActiveOrganizationMembership(ctx, id.PersonID)
	if sponsorErr != nil {
		ctx.Err.Printf("/dashboard/hackathons sponsor memberships for %s: %s", id.PersonID, sponsorErr)
	}
	if id.Speaker != nil {
		if strings.TrimSpace(id.Speaker.Name) != "" {
			name = id.Speaker.Name
		}
		photo = id.Speaker.Photo
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_hackathons.tmpl", &DashboardPage{
		Name:                    name,
		Photo:                   photo,
		Email:                   encodedEmail,
		HMAC:                    encodedHMAC,
		HackathonProjects:       buildDashboardHackathonProjects(participantProjects),
		HasHackathonProjects:    true,
		HasSponsorOrganizations: hasSponsorMembership,
		IsGlobalAdmin:           isGlobalAdmin,
		Year:                    helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/dashboard/hackathons render: %s", err)
		http.Error(w, "Unable to load hackathon projects", http.StatusInternalServerError)
	}
}

func buildDashboardHackathonProjects(entries []*getters.HackathonParticipantProject) []*DashboardHackathonProject {
	out := make([]*DashboardHackathonProject, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.Project == nil || entry.Conf == nil {
			continue
		}
		projectBase := "/" + url.PathEscape(entry.Conf.Tag) + "/hackathon/projects/" + url.PathEscape(entry.Project.ID)
		imageURL := strings.TrimSpace(entry.Project.ImageURL)
		if imageURL == "" && len(entry.Project.ImageURLs) > 0 {
			imageURL = strings.TrimSpace(entry.Project.ImageURLs[0])
		}
		out = append(out, &DashboardHackathonProject{
			Project:          entry.Project,
			Conf:             entry.Conf,
			CompetitionTitle: entry.CompetitionTitle,
			MemberRole:       entry.MemberRole,
			TeamSize:         entry.TeamSize,
			ProjectURL:       projectBase,
			EditURL:          projectBase + "/edit",
			TeamURL:          projectBase + "/edit#team",
			SubmissionURL:    projectBase + "/edit#submission",
			HackathonURL:     "/" + url.PathEscape(entry.Conf.Tag) + "/hackathon",
			StatusLabel:      dashboardHackathonProjectStatusLabel(entry.Project.Status),
			ImageURL:         imageURL,
		})
	}
	return out
}

func dashboardHackathonProjectStatusLabel(status string) string {
	switch status {
	case getters.ProjectStatusSubmitted:
		return "Submitted"
	case getters.ProjectStatusAdvanced:
		return "Advanced"
	case getters.ProjectStatusHidden:
		return "Hidden"
	default:
		return "Draft"
	}
}

func DashboardHackathonTickets(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id, err := auth.Resolve(r, ctx)
	if err != nil || id == nil || id.Speaker == nil {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("A person profile is required to view ticket awards"), http.StatusSeeOther)
		return
	}
	email := dashboardIdentityEmail(id)
	people := []*types.Speaker{id.Speaker}
	entitlements, err := dashboardTicketEntitlements(ctx, people)
	if err != nil {
		ctx.Err.Printf("/dashboard/tickets entitlements: %s", err)
		http.Error(w, "Unable to load ticket awards", http.StatusInternalServerError)
		return
	}
	unclaimedEntitlements := 0
	for _, entitlement := range entitlements {
		if entitlement != nil && entitlement.ClaimedAt == nil && entitlement.VoidedAt == nil {
			unclaimedEntitlements++
		}
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("/dashboard/tickets conferences: %s", err)
		http.Error(w, "Unable to load upcoming events", http.StatusInternalServerError)
		return
	}
	name := email
	for _, person := range people {
		if person != nil && strings.TrimSpace(person.Name) != "" {
			name = person.Name
			break
		}
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_tickets.tmpl", &DashboardPage{
		Name:                        name,
		Speaker:                     people[0],
		TicketEntitlements:          entitlements,
		UnclaimedTicketEntitlements: unclaimedEntitlements,
		TicketClaimConfs:            buyableConfs(confs),
		FlashMessage:                r.URL.Query().Get("flash"),
		FlashError:                  r.URL.Query().Get("error"),
		Year:                        helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/dashboard/tickets template: %s", err)
		http.Error(w, "Unable to load ticket awards", http.StatusInternalServerError)
	}
}

func dashboardTicketEntitlements(ctx *config.AppContext, people []*types.Speaker) ([]*types.HackathonTicketEntitlement, error) {
	seen := map[string]bool{}
	var out []*types.HackathonTicketEntitlement
	for _, person := range people {
		if person == nil || strings.TrimSpace(person.ID) == "" {
			continue
		}
		entitlements, err := getters.ListTicketEntitlementsForPerson(ctx, person.ID)
		if err != nil {
			return nil, err
		}
		for _, entitlement := range entitlements {
			if entitlement == nil || seen[entitlement.ID] {
				continue
			}
			seen[entitlement.ID] = true
			out = append(out, entitlement)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].ClaimedAt == nil) != (out[j].ClaimedAt == nil) {
			return out[i].ClaimedAt == nil
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func DashboardOrders(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, encodedEmail, encodedHMAC, ok := dashboardRequestIdentity(w, r, ctx)
	if !ok {
		return
	}
	id, resolveErr := auth.Resolve(r, ctx)
	if resolveErr != nil || id == nil {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("A person profile is required to view orders."), http.StatusSeeOther)
		return
	}
	orders, err := getters.ListShopOrdersForPerson(ctx, id.PersonID, 100)
	if err != nil {
		ctx.Err.Printf("/dashboard/orders for %s: %s", email, err)
		http.Error(w, "Unable to load orders", http.StatusInternalServerError)
		return
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_orders.tmpl", &DashboardPage{
		Name:       email,
		Email:      encodedEmail,
		HMAC:       encodedHMAC,
		ShopOrders: orders,
		BaseURI:    ctx.Env.GetURI(),
		Year:       helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/dashboard/orders render: %s", err)
		http.Error(w, "Unable to load orders", http.StatusInternalServerError)
	}
}

func DashboardOrder(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, encodedEmail, encodedHMAC, ok := dashboardRequestIdentity(w, r, ctx)
	if !ok {
		return
	}
	publicID := strings.TrimSpace(mux.Vars(r)["order"])
	order, err := getters.GetShopOrderByPublicID(ctx, publicID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orders/%s: %s", publicID, err)
		http.NotFound(w, r)
		return
	}
	id, resolveErr := auth.Resolve(r, ctx)
	if resolveErr != nil || id == nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	owns, ownErr := getters.PersonOwnsShopOrder(ctx, id.PersonID, order.ID)
	if ownErr != nil || !owns {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_order.tmpl", &DashboardPage{
		Name:      email,
		Email:     encodedEmail,
		HMAC:      encodedHMAC,
		ShopOrder: order,
		BaseURI:   ctx.Env.GetURI(),
		Year:      helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/dashboard/orders/%s render: %s", publicID, err)
		http.Error(w, "Unable to load order", http.StatusInternalServerError)
	}
}

func DashboardOrderReceipt(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, encodedEmail, encodedHMAC, ok := dashboardRequestIdentity(w, r, ctx)
	if !ok {
		return
	}
	publicID := strings.TrimSpace(mux.Vars(r)["order"])
	order, err := getters.GetShopOrderByPublicID(ctx, publicID)
	if err != nil {
		ctx.Err.Printf("/dashboard/orders/%s/receipt: %s", publicID, err)
		http.NotFound(w, r)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(order.BuyerEmail), strings.TrimSpace(email)) {
		http.Error(w, "receipt not found", http.StatusNotFound)
		return
	}
	var html bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&html, "shop/receipt.tmpl", &DashboardPage{
		Name:      email,
		Email:     encodedEmail,
		HMAC:      encodedHMAC,
		ShopOrder: order,
		BaseURI:   ctx.Env.GetURI(),
		Year:      helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/dashboard/orders/%s/receipt render: %s", publicID, err)
		http.Error(w, "Could not render receipt", http.StatusInternalServerError)
		return
	}

	tmp, err := os.CreateTemp("", "btcpp-receipt-*.html")
	if err != nil {
		ctx.Err.Printf("/dashboard/orders/%s/receipt temp: %s", publicID, err)
		http.Error(w, "Could not generate receipt PDF", http.StatusInternalServerError)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(html.Bytes()); err != nil {
		tmp.Close()
		ctx.Err.Printf("/dashboard/orders/%s/receipt temp write: %s", publicID, err)
		http.Error(w, "Could not generate receipt PDF", http.StatusInternalServerError)
		return
	}
	if err := tmp.Close(); err != nil {
		ctx.Err.Printf("/dashboard/orders/%s/receipt temp close: %s", publicID, err)
		http.Error(w, "Could not generate receipt PDF", http.StatusInternalServerError)
		return
	}

	fileURL := (&url.URL{Scheme: "file", Path: tmpName}).String()
	pdfBytes, err := helpers.BuildChromePdf(ctx, &helpers.PDFPage{
		URL:    fileURL,
		Width:  8.5,
		Height: 11,
	})
	if err != nil {
		ctx.Err.Printf("/dashboard/orders/%s/receipt pdf: %s", publicID, err)
		http.Error(w, "Could not generate receipt PDF", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("btcpp-%s-receipt.pdf", order.PublicID)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfBytes)))
	w.Write(pdfBytes)
}

func dashboardRequestIdentity(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (email, encodedEmail, encodedHMAC string, ok bool) {
	email, encodedHMAC, err := validateVolEmail(r, ctx)
	encodedEmail = ""
	if err != nil {
		redirectDashboardLogin(w, r, "")
		return "", "", "", false
	}
	return email, encodedEmail, encodedHMAC, true
}

func redirectDashboardLogin(w http.ResponseWriter, r *http.Request, errorMessage string) {
	query := url.Values{"next": {r.URL.Path}}
	if flash := strings.TrimSpace(r.URL.Query().Get("flash")); flash != "" {
		query.Set("flash", flash)
	}
	if errorMessage != "" {
		query.Set("error", errorMessage)
	}
	http.Redirect(w, r, "/login?"+query.Encode(), http.StatusSeeOther)
}

func dashboardDevLoginEnabled(ctx *config.AppContext) bool {
	return ctx != nil && ctx.Env != nil && !ctx.InProduction && !ctx.Env.Prod
}

func hasPublicWhoIsProfile(ctx *config.AppContext, speaker *types.Speaker) bool {
	_, ok := resolvedWhoIsPublicID(ctx, speaker)
	return ok
}

func attachDashboardAdminRoles(blocks []*EventBlock, id *auth.Identity) {
	if id == nil {
		return
	}
	for _, b := range blocks {
		if b == nil || b.Conf == nil {
			continue
		}
		switch {
		case id.HasRoleForConf(b.Conf.Tag, auth.RoleAdmin):
			b.AdminRole = auth.RoleAdmin
		case id.HasRoleForConf(b.Conf.Tag, auth.RoleVolcoord):
			b.AdminRole = auth.RoleVolcoord
		}
	}
}

func attachDashboardHackathonManagerRoles(active, past []*EventBlock, confs []*types.Conf, id *auth.Identity) ([]*EventBlock, []*EventBlock) {
	if id == nil {
		return active, past
	}
	byTag := make(map[string]*EventBlock, len(active)+len(past))
	for _, b := range append(append([]*EventBlock(nil), active...), past...) {
		if b == nil || b.Conf == nil || !b.Conf.ShowHackathon {
			continue
		}
		b.HackathonManager = id.HasExactRoleForConf(b.Conf.Tag, auth.RoleHackathon)
		byTag[b.Conf.Tag] = b
	}
	for _, conf := range confs {
		if conf == nil || !conf.ShowHackathon || byTag[conf.Tag] != nil || !id.HasExactRoleForConf(conf.Tag, auth.RoleHackathon) {
			continue
		}
		block := &EventBlock{Conf: conf, CanBuy: conf.Active && conf.InFuture(), HackathonManager: true}
		if conf.DashboardHasEnded() {
			past = append(past, block)
		} else {
			active = append(active, block)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].Conf.StartDate.Before(active[j].Conf.StartDate)
	})
	sort.Slice(past, func(i, j int) bool {
		return past[i].Conf.StartDate.After(past[j].Conf.StartDate)
	})
	return active, past
}

func attachRegistrationQRCodes(ctx *config.AppContext, regs []*types.Registration) {
	for _, reg := range regs {
		if reg == nil || reg.RefID == "" || reg.QRCodeURI != "" || types.IsSponsoredTicketType(reg.Type) {
			continue
		}
		qr, err := ticketQRCodeURI(ctx, reg.RefID)
		if err != nil {
			ctx.Err.Printf("dashboard ticket qr %s: %s", reg.RefID, err)
			continue
		}
		reg.QRCodeURI = qr
	}
}

func archiveTalks(sc *types.SpeakerConf) []*types.Proposal {
	if sc == nil {
		return nil
	}
	out := make([]*types.Proposal, 0, len(sc.Proposals))
	for _, proposal := range sc.Proposals {
		if proposal == nil {
			continue
		}
		if proposal.Status == StatusAccepted || proposal.Status == StatusScheduled {
			out = append(out, proposal)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		leftSched := left != nil && left.ConfTalk != nil && left.ConfTalk.Sched != nil
		rightSched := right != nil && right.ConfTalk != nil && right.ConfTalk.Sched != nil
		if leftSched && rightSched {
			if !left.ConfTalk.Sched.Start.Equal(right.ConfTalk.Sched.Start) {
				return left.ConfTalk.Sched.Start.Before(right.ConfTalk.Sched.Start)
			}
			return left.Title < right.Title
		}
		if leftSched != rightSched {
			return leftSched
		}
		return false
	})
	return out
}

func archiveResourcesAllowed(proposal *types.Proposal) bool {
	if proposal == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(proposal.TalkType)) {
	case "talk", "workshop", "keynote":
		return true
	default:
		return false
	}
}

func dashboardArchiveYears(blocks []*EventBlock) ([]*DashboardArchiveYear, int) {
	byYear := map[int]*DashboardArchiveYear{}
	totalSessions := 0
	for _, block := range blocks {
		if block == nil || block.Conf == nil {
			continue
		}
		year := block.Conf.StartDate.In(block.Conf.Loc()).Year()
		if year == 0 {
			continue
		}
		bucket := byYear[year]
		if bucket == nil {
			bucket = &DashboardArchiveYear{Year: year}
			byYear[year] = bucket
		}
		sessions := 0
		if block.SpeakerConf != nil {
			for _, proposal := range block.SpeakerConf.Proposals {
				if proposal == nil {
					continue
				}
				if proposal.Status == StatusAccepted || proposal.Status == StatusScheduled {
					sessions++
				}
			}
		}
		bucket.SessionCount += sessions
		totalSessions += sessions
		bucket.Blocks = append(bucket.Blocks, block)
	}

	years := make([]int, 0, len(byYear))
	for year := range byYear {
		years = append(years, year)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))

	out := make([]*DashboardArchiveYear, 0, len(years))
	for _, year := range years {
		bucket := byYear[year]
		sort.SliceStable(bucket.Blocks, func(i, j int) bool {
			return bucket.Blocks[i].Conf.StartDate.After(bucket.Blocks[j].Conf.StartDate)
		})
		out = append(out, bucket)
	}
	return out, totalSessions
}

func appendLegacyArchiveConfs(confs []*types.Conf) []*types.Conf {
	seen := make(map[string]bool, len(confs))
	for _, conf := range confs {
		if conf != nil {
			seen[conf.Tag] = true
		}
	}
	for _, conf := range legacyArchiveConfs() {
		if conf != nil && !seen[conf.Tag] {
			confs = append(confs, conf)
			seen[conf.Tag] = true
		}
	}
	return confs
}

func legacyArchiveConfs() []*types.Conf {
	return []*types.Conf{
		{
			Ref:       "atx22",
			Tag:       "atx22",
			Desc:      "bitcoin++ Austin",
			DateDesc:  "2022",
			StartDate: time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2022, 1, 2, 0, 0, 0, 0, time.UTC),
			Location:  "Austin, Texas",
		},
		{
			Ref:       "cdmx22",
			Tag:       "cdmx22",
			Desc:      "bitcoin++ Mexico City",
			DateDesc:  "2022",
			StartDate: time.Date(2022, 2, 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2022, 2, 2, 0, 0, 0, 0, time.UTC),
			Location:  "Mexico City, Mexico",
		},
		{
			Ref:       "berlin23",
			Tag:       "berlin23",
			Desc:      "bitcoin++ Berlin",
			DateDesc:  "2023",
			StartDate: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC),
			Location:  "Berlin, Germany",
		},
	}
}

// dashboardIdentity picks a name + hometown to greet the user with. Prefers
// the Speaker record (its Name is curated) and falls back to the first
// volunteer app. Hometown lives on SpeakerConf (ComingFrom) for speakers
// and directly on Volunteer for shift workers.
func dashboardIdentity(speakers []*types.Speaker, speakerConfs []*types.SpeakerConf, volapps []*types.Volunteer) (string, string) {
	if len(speakers) > 0 && speakers[0].Name != "" {
		name := speakers[0].Name
		hometown := ""
		for _, sc := range speakerConfs {
			if sc.ComingFrom != "" {
				hometown = sc.ComingFrom
				break
			}
		}
		if hometown == "" && len(volapps) > 0 {
			hometown = volapps[0].Hometown
		}
		return name, hometown
	}
	if len(volapps) > 0 {
		return volapps[0].Name, volapps[0].Hometown
	}
	return "there", ""
}

// enrichDashboardProposals walks every proposal across the user's
// SpeakerConfs and attaches the data needed by the talk card:
//
//   - proposal.Speakers: full SpeakerConf+Speaker for every speaker on the
//     proposal (so we can render avatars).
//   - proposal.ConfTalk: the ConfTalk row for accepted proposals (Clipart).
//   - proposal.Recording: the Recording row when one exists (YT link).
//
// Two-phase to keep related work bounded: first batch-fetch every unique
// co-speaker's SpeakerConf+Speaker, then fan-out per-proposal enrich
// (ConfTalk → Recording is a serial chain within each proposal goroutine).
//
// Best-effort — individual fetches that fail just leave the field nil. The
// dashboard renders without that piece rather than 500ing.
func enrichDashboardProposals(ctx *config.AppContext, speakerConfs []*types.SpeakerConf) {
	scCache := make(map[string]*types.SpeakerConf)
	// Seed the cache with the user's own SpeakerConfs (their Speaker is
	// already resolved by GetSpeakerConfsByEmail) so we don't re-fetch.
	for _, sc := range speakerConfs {
		if sc != nil {
			scCache[sc.ID] = sc
		}
	}

	// Walk proposals once to collect unique work items + which proposals
	// to enrich. Avoids enriching the same proposal twice when shared
	// across the user's SpeakerConfs (rare, but cheap to defend).
	uniqueRefs := make(map[string]struct{})
	seenProp := make(map[string]bool)
	var proposals []*types.Proposal
	for _, sc := range speakerConfs {
		for _, p := range sc.Proposals {
			if p == nil || seenProp[p.ID] {
				continue
			}
			seenProp[p.ID] = true
			proposals = append(proposals, p)
			for _, ref := range p.SpeakerConfRefs {
				if _, ok := scCache[ref]; ok {
					continue
				}
				uniqueRefs[ref] = struct{}{}
			}
		}
	}

	// Phase 1: batch-fetch every unique co-speaker SpeakerConf. The previous
	// one-goroutine-per-speaker path multiplied database queries and pool
	// pressure for accounts with many collaborative talks.
	t1 := time.Now()
	if len(uniqueRefs) > 0 {
		refs := make([]string, 0, len(uniqueRefs))
		for ref := range uniqueRefs {
			refs = append(refs, ref)
		}
		speakerRows, err := getters.ListSpeakerConfsByIDs(ctx, refs, nil, nil)
		if err != nil {
			ctx.Err.Printf("enrich: fetch %d co-speaker rows: %s", len(refs), err)
		} else {
			for _, sc := range speakerRows {
				if sc == nil {
					continue
				}
				scCache[sc.ID] = sc
			}
		}
	}
	ctx.Infos.Printf("enrich phase1 (%d co-speaker scs): %s", len(uniqueRefs), time.Since(t1))

	// Phase 2: parallel-enrich each proposal. Cache is now read-only —
	// each goroutine attaches its own ConfTalk + Recording chain.
	t2 := time.Now()
	var wg sync.WaitGroup
	for _, p := range proposals {
		wg.Add(1)
		go func(p *types.Proposal) {
			defer wg.Done()
			enrichProposal(ctx, p, scCache)
		}(p)
	}
	wg.Wait()
	ctx.Infos.Printf("enrich phase2 (%d proposals): %s", len(proposals), time.Since(t2))
}

// enrichProposal attaches Speakers (from the prebuilt cache), ConfTalk,
// and Recording to a single proposal. Safe to call concurrently across
// proposals — only the proposal's own fields are mutated and scCache is
// read-only at this point.
func enrichProposal(ctx *config.AppContext, p *types.Proposal, scCache map[string]*types.SpeakerConf) {
	p.Speakers = nil
	for _, refID := range p.SpeakerConfRefs {
		if sc := scCache[refID]; sc != nil {
			p.Speakers = append(p.Speakers, sc)
		}
	}

	// Both Accepted (admin draft) and Scheduled (cal invite sent)
	// have a ConfTalk row that the dashboard wants to surface — clipart
	// in the card thumbnail and the "Add to calendar" picker for the
	// Scheduled branch. Pre-Accepted statuses have no ConfTalk yet;
	// terminal-decline statuses keep one but we don't need the
	// enrichment for them.
	if p.Status != StatusAccepted && p.Status != StatusScheduled {
		return
	}
	ct, err := getters.GetConfTalkByProposal(ctx, p.ID)
	if err != nil {
		ctx.Err.Printf("enrich proposal %s: conftalk: %s", p.ID, err)
		return
	}
	p.ConfTalk = ct
	if ct == nil {
		return
	}
	rec, err := getters.GetRecordingByConfTalk(ctx, ct.ID)
	if err != nil {
		ctx.Err.Printf("enrich proposal %s: recording: %s", p.ID, err)
		return
	}
	p.Recording = rec
}

// buildEventBlocks consolidates the user's per-event relationships
// (speaker conf, volunteer app, tickets) into one EventBlock per conf
// they touch. Returns separate active / past slices so the template
// can render past confs in a collapsed bucket.
//
// Sort order within each slice is by conf StartDate ascending — the
// nearest upcoming conf appears first in active; oldest first in past.
//
// A conf can appear in either slice but never both. If a conf has no
// relationship at all (the user never applied / volunteered / bought)
// it doesn't get a block; those confs surface via EligibleConfs /
// BuyableConfs in the discover section instead.
func buildEventBlocks(
	speakerConfs []*types.SpeakerConf,
	volApps []*types.Volunteer,
	tickets []*UserTicket,
	regs []*types.Registration,
	confs []*types.Conf,
	volInfos map[string]*types.VolInfo,
) (active, past []*EventBlock) {
	byTag := make(map[string]*EventBlock)
	confByTag := make(map[string]*types.Conf, len(confs))
	for _, c := range confs {
		if c != nil {
			confByTag[c.Tag] = c
		}
	}

	block := func(conf *types.Conf) *EventBlock {
		if conf == nil {
			return nil
		}
		if eb, ok := byTag[conf.Tag]; ok {
			return eb
		}
		eb := &EventBlock{
			Conf:   conf,
			CanBuy: conf.Active && conf.InFuture(),
		}
		byTag[conf.Tag] = eb
		return eb
	}

	for _, sc := range speakerConfs {
		conf := speakerConfConf(sc)
		if eb := block(conf); eb != nil {
			eb.SpeakerConf = sc
		}
	}

	for _, vol := range volApps {
		if len(vol.ScheduleFor) == 0 {
			continue
		}
		conf := vol.ScheduleFor[0]
		if eb := block(conf); eb != nil {
			eb.VolApp = vol
			if vi, ok := volInfos[conf.Tag]; ok {
				eb.VolInfo = vi
			}
		}
	}

	// Tickets are scoped via the resolved Conf on each UserTicket.
	// upcomingTickets already filtered out past confs, so to also
	// surface tickets for ended confs in the past block we walk
	// the raw regs slice independently.
	for _, t := range tickets {
		if eb := block(t.Conf); eb != nil {
			eb.Tickets = append(eb.Tickets, t.Reg)
		}
	}
	for _, r := range regs {
		if r == nil || r.RefID == "" {
			continue
		}
		conf := confByRef(confByTag, r.ConfRef)
		if conf == nil {
			continue
		}
		eb := block(conf)
		if eb == nil {
			continue
		}
		// Avoid double-add when upcomingTickets already covered it.
		if !containsTicket(eb.Tickets, r) {
			eb.Tickets = append(eb.Tickets, r)
		}
	}

	for _, eb := range byTag {
		if eb.Conf != nil && eb.Conf.DashboardHasEnded() {
			past = append(past, eb)
		} else {
			active = append(active, eb)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].Conf.StartDate.Before(active[j].Conf.StartDate)
	})
	sort.Slice(past, func(i, j int) bool {
		return past[i].Conf.StartDate.After(past[j].Conf.StartDate)
	})
	return active, past
}

func attachSatelliteEventBlocks(active, past []*EventBlock, events []*types.SatelliteEvent, confs []*types.Conf) ([]*EventBlock, []*EventBlock) {
	if len(events) == 0 {
		return active, past
	}
	confByRef := make(map[string]*types.Conf, len(confs))
	for _, c := range confs {
		if c != nil {
			confByRef[c.Ref] = c
			confByRef[c.Tag] = c
		}
	}
	byTag := make(map[string]*EventBlock, len(active)+len(past))
	for _, b := range active {
		if b != nil && b.Conf != nil {
			byTag[b.Conf.Tag] = b
		}
	}
	for _, b := range past {
		if b != nil && b.Conf != nil {
			byTag[b.Conf.Tag] = b
		}
	}
	for _, ev := range events {
		if ev == nil {
			continue
		}
		conf := confByRef[ev.ConfRef]
		if conf == nil {
			continue
		}
		eb := byTag[conf.Tag]
		if eb == nil {
			eb = &EventBlock{
				Conf:   conf,
				CanBuy: conf.Active && conf.InFuture(),
			}
			byTag[conf.Tag] = eb
			if conf.DashboardHasEnded() {
				past = append(past, eb)
			} else {
				active = append(active, eb)
			}
		}
		eb.SatelliteEvents = append(eb.SatelliteEvents, ev)
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].Conf.StartDate.Before(active[j].Conf.StartDate)
	})
	sort.Slice(past, func(i, j int) bool {
		return past[i].Conf.StartDate.After(past[j].Conf.StartDate)
	})
	return active, past
}

func attachJudgeEventBlocks(active, past []*EventBlock, assignments []*types.CompetitionJudgeAssignment, confs []*types.Conf) ([]*EventBlock, []*EventBlock) {
	if len(assignments) == 0 {
		return active, past
	}
	confByRef := make(map[string]*types.Conf, len(confs)*2)
	for _, c := range confs {
		if c != nil {
			confByRef[c.Ref] = c
			confByRef[c.Tag] = c
		}
	}
	byTag := make(map[string]*EventBlock, len(active)+len(past))
	for _, b := range active {
		if b != nil && b.Conf != nil {
			byTag[b.Conf.Tag] = b
		}
	}
	for _, b := range past {
		if b != nil && b.Conf != nil {
			byTag[b.Conf.Tag] = b
		}
	}
	for _, assignment := range assignments {
		if assignment == nil {
			continue
		}
		conf := confByRef[assignment.ConferenceID]
		if conf == nil {
			conf = confByRef[assignment.ConferenceTag]
		}
		if conf == nil {
			continue
		}
		eb := byTag[conf.Tag]
		if eb == nil {
			eb = &EventBlock{Conf: conf, CanBuy: conf.Active && conf.InFuture()}
			byTag[conf.Tag] = eb
			if conf.DashboardHasEnded() {
				past = append(past, eb)
			} else {
				active = append(active, eb)
			}
		}
		if !containsString(eb.JudgeTypes, assignment.JudgeType) {
			if assignment.JudgeType != "" {
				eb.JudgeTypes = append(eb.JudgeTypes, assignment.JudgeType)
			}
		}
		eb.SponsorJudge = eb.SponsorJudge || assignment.SponsorAward
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].Conf.StartDate.Before(active[j].Conf.StartDate)
	})
	sort.Slice(past, func(i, j int) bool {
		return past[i].Conf.StartDate.After(past[j].Conf.StartDate)
	})
	return active, past
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// confByRef finds a Conf by Notion page-ID (the value stored on
// PurchasesDb rows). Linear scan over the typically-small confs map.
func confByRef(byTag map[string]*types.Conf, ref string) *types.Conf {
	for _, c := range byTag {
		if c != nil && (c.Ref == ref || c.Tag == ref) {
			return c
		}
	}
	return nil
}

func containsTicket(list []*types.Registration, r *types.Registration) bool {
	for _, t := range list {
		if t != nil && t.RefID == r.RefID {
			return true
		}
	}
	return false
}

// excludeConfsInBlocks filters a candidate slice (e.g. EligibleConfs)
// to drop confs that already appear as event blocks — the discovery
// list at the bottom of the dashboard shouldn't repeat events the
// user is already engaged with.
func excludeConfsInBlocks(candidates []*types.Conf, blocks []*EventBlock) []*types.Conf {
	if len(blocks) == 0 {
		return candidates
	}
	seen := make(map[string]bool, len(blocks))
	for _, eb := range blocks {
		if eb != nil && eb.Conf != nil {
			seen[eb.Conf.Tag] = true
		}
	}
	out := make([]*types.Conf, 0, len(candidates))
	for _, c := range candidates {
		if c == nil || seen[c.Tag] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// upcomingTickets joins the user's PurchasesDb rows with the confs cache and
// keeps them through the dashboard's post-event grace period.
func upcomingTickets(regs []*types.Registration, allConfs []*types.Conf) []*UserTicket {
	if len(regs) == 0 {
		return nil
	}
	confByRef := make(map[string]*types.Conf, len(allConfs))
	for _, c := range allConfs {
		if c != nil {
			confByRef[c.Ref] = c
			confByRef[c.Tag] = c
		}
	}
	var out []*UserTicket
	for _, r := range regs {
		if r == nil || r.RefID == "" {
			continue
		}
		c := confByRef[r.ConfRef]
		if c == nil || c.DashboardHasEnded() {
			continue
		}
		out = append(out, &UserTicket{Reg: r, Conf: c})
	}
	return out
}

// discoverConfs returns every Active+InFuture conf the user has no
// existing relationship with — drives the dashboard's per-event
// discover cards. Each card renders three CTAs (Get ticket / Apply
// to speak / Apply to volunteer) gated independently in the template,
// so we don't pre-filter by which CTAs are enabled — we just want the
// full list of confs to surface.
func discoverConfs(allConfs []*types.Conf, blocks []*EventBlock) []*types.Conf {
	inBlock := map[string]bool{}
	for _, b := range blocks {
		if b != nil && b.Conf != nil {
			inBlock[b.Conf.Tag] = true
		}
	}
	var out []*types.Conf
	for _, c := range allConfs {
		if c == nil || !c.Active || !c.InFuture() {
			continue
		}
		if inBlock[c.Tag] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// buyableConfs returns Active confs whose start date is still in the
// future — i.e., the ones a logged-in user can still buy a ticket for.
// We don't filter by existing purchases; the conf page handles "you've
// already got a ticket" UI when the user clicks through.
func buyableConfs(allConfs []*types.Conf) []*types.Conf {
	var out []*types.Conf
	for _, c := range allConfs {
		if c == nil || !c.Active || !c.InFuture() {
			continue
		}
		out = append(out, c)
	}
	return out
}

// eligibleApplyConfs returns confs the user could still apply to speak at:
// Active, applications still open (TalksOpen), and no existing SpeakerConf
// linking them. Used to render the dashboard's "Apply to speak" section.
func eligibleApplyConfs(allConfs []*types.Conf, userSpeakerConfs []*types.SpeakerConf) []*types.Conf {
	applied := make(map[string]bool)
	for _, sc := range userSpeakerConfs {
		if conf := speakerConfConf(sc); conf != nil {
			applied[conf.Tag] = true
		}
	}
	var out []*types.Conf
	for _, c := range allConfs {
		if c == nil || !c.TalksOpen() || applied[c.Tag] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// splitSpeakerConfsByEnded partitions speaker confs using the dashboard's
// post-event grace period. A SpeakerConf with no resolvable conf
// (no proposals or proposals without ScheduleFor) lands in the active
// bucket so it's still visible — better to show too much than to bury it.
func splitSpeakerConfsByEnded(scs []*types.SpeakerConf) (active, past []*types.SpeakerConf) {
	for _, sc := range scs {
		conf := speakerConfConf(sc)
		if conf != nil && conf.DashboardHasEnded() {
			past = append(past, sc)
		} else {
			active = append(active, sc)
		}
	}
	return active, past
}

func splitVolAppsByEnded(vols []*types.Volunteer) (active, past []*types.Volunteer) {
	for _, v := range vols {
		if len(v.ScheduleFor) > 0 && v.ScheduleFor[0] != nil && v.ScheduleFor[0].DashboardHasEnded() {
			past = append(past, v)
		} else {
			active = append(active, v)
		}
	}
	return active, past
}

// speakerConfConf returns the conf this SpeakerConf belongs to, looking it
// up via the first proposal's ScheduleFor. SpeakerConfs are per-(speaker,
// conf) so all proposals share the same conf — but defensive against the
// no-proposal edge case.
func speakerConfConf(sc *types.SpeakerConf) *types.Conf {
	for _, p := range sc.Proposals {
		if p != nil && p.ScheduleFor != nil {
			return p.ScheduleFor
		}
	}
	return nil
}

// calcDashboardStats counts unique proposals (by ID — a proposal can appear
// under multiple SpeakerConfs in multi-speaker setups), volunteer conferences,
// and selected shifts.
func calcDashboardStats(speakerConfs []*types.SpeakerConf, volapps []*types.Volunteer) *DashboardStats {
	s := &DashboardStats{}
	seen := make(map[string]bool)
	for _, sc := range speakerConfs {
		for _, p := range sc.Proposals {
			if p == nil || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			s.TalksApplied++
			if p.Status == StatusAccepted || p.Status == StatusScheduled {
				s.TalksAccepted++
			}
		}
	}
	for _, v := range volapps {
		s.ShiftsApplied++
		s.ShiftsBooked += len(v.WorkShifts)
	}
	return s
}

// loadAffiliateCode returns the user's live (non-archived) affiliate
// DiscountCode, or nil when the gate is closed (no tickets) / they
// haven't made one yet / the cache lookup blipped.
func loadAffiliateCode(ctx *config.AppContext, personID, email string, eligible bool) *types.DiscountCode {
	if !eligible || email == "" {
		return nil
	}
	var code *types.DiscountCode
	var err error
	if personID != "" {
		code, err = getters.GetDiscountByAffiliatePersonID(ctx, personID)
	} else {
		code, err = getters.FindAffiliateCodeByEmail(ctx, email)
	}
	if err != nil {
		ctx.Err.Printf("/dashboard affiliate lookup %s: %s", email, err)
		return nil
	}
	return code
}

// loadAffiliateStats sums every AffiliateUsage row for the user via
// a live database query (no cache, since affiliates expect to see
// their freshest stats on refresh). Returns zeros when the gate is
// closed; the template renders zeros as "0 tickets sold / $0".
func loadAffiliateStats(ctx *config.AppContext, personID, email string, eligible bool) *AffiliateStats {
	if !eligible || email == "" {
		return &AffiliateStats{}
	}
	var totals getters.AffiliateStatsTotals
	var err error
	if personID != "" {
		totals, err = getters.SumAffiliateStatsForPerson(ctx, personID)
	} else {
		totals, err = getters.SumAffiliateStatsByEmail(ctx, email)
	}
	if err != nil {
		ctx.Err.Printf("/dashboard affiliate stats %s: %s", email, err)
		return &AffiliateStats{}
	}
	return &AffiliateStats{
		TicketsSold: totals.TicketsSold,
		SavedSats:   totals.SavedSats,
		EarnedSats:  totals.EarnedSats,
	}
}
