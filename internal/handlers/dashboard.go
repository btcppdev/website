package handlers

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

// Dashboard is the magic-link-authed self-service page combining a speaker's
// talk applications + their volunteer shift signups.
//
// GET without valid HMAC → renders the email-entry form.
// GET with valid HMAC → loads SpeakerConfs + VolunteerApps for the email.
// POST with email → emails a magic link, redirects home.
func Dashboard(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		dec := newFormDecoder()
		var form EmailForm
		if err := dec.Decode(&form, r.PostForm); err != nil {
			ctx.Err.Printf("/dashboard form decode failed: %s", err)
			w.Write([]byte(helpers.ErrVolApp("Unable to send you email link.")))
			return
		}
		if _, err := emails.OnlyForLogin(ctx, form.Email); err != nil {
			http.Error(w, "Unable to send login link via email", http.StatusInternalServerError)
			ctx.Err.Printf("/dashboard onlyforlogin failed: %s", err)
			return
		}
		http.Redirect(w, r, "/dashboard?flash="+url.QueryEscape("Check your inbox — we sent you a login link."), http.StatusSeeOther)
		return
	}

	// Two equally valid auth paths land here:
	//
	//   1. Magic-link click with ?em=&hr= URL params — the
	//      magic-link's HMAC validates the email; we stamp the
	//      session for subsequent navigation.
	//   2. Already-authed visitor (the affiliate / role-manager
	//      flows redirect back here without rebuilding the URL
	//      params) — session has the email; we mint a fresh
	//      ?em=&hr= pair from it so dashboard sub-pages that
	//      still hand-build URLs (talks, vol shifts, etc.) keep
	//      working.
	email, encodedHMAC, err := validateVolEmail(r, ctx)
	validatedByLink := err == nil
	encodedEmail := r.URL.Query().Get("em")
	if err != nil {
		// Fall back to the SCS session before giving up.
		if sessEmail := ctx.Session.GetString(r.Context(), auth.SessionEmailKey); sessEmail != "" {
			email = sessEmail
			encodedHMAC = base64.RawURLEncoding.EncodeToString([]byte(helpers.CreateEmailHMAC(ctx, email)))
			encodedEmail = base64.RawURLEncoding.EncodeToString([]byte(email))
			err = nil
		}
	}
	if err != nil {
		ctx.Infos.Printf("/dashboard HMAC validation failed: %s", err)
		renderDashboardLogin(w, r, ctx)
		return
	}

	// The magic-link URL HMAC is itself proof of identity — stamp the
	// session only when this request actually carried a valid link.
	// If we already fell back to the session, renewing the token again
	// on every dashboard page load just churns cookies.
	if validatedByLink {
		if err := auth.LoginEmail(ctx, r, email); err != nil {
			ctx.Err.Printf("/dashboard session stamp for %s: %s", email, err)
		}
	}

	dashStart := time.Now()
	reqID := requestID(r)
	defer func() {
		ctx.Infos.Printf("/dashboard id=%s total=%s", reqID, time.Since(dashStart))
	}()

	// Top-level fan-out: speakerconfs + volunteer apps + user's tickets
	// are all independent.
	var (
		speakers     []*types.Speaker
		speakerConfs []*types.SpeakerConf
		scErr        error
		volapps      []*types.Volunteer
		volErr       error
		regs         []*types.Registration
		regErr       error
		satEvents    []*types.SatelliteEvent
		satErr       error
	)
	t1 := time.Now()
	var topWg sync.WaitGroup
	topWg.Add(4)
	var scDur, volDur, regDur, satDur time.Duration
	go func() {
		defer topWg.Done()
		s := time.Now()
		speakers, speakerConfs, scErr = getters.GetSpeakerConfsByEmail(ctx, email)
		scDur = time.Since(s)
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		volapps, volErr = getters.ListVolunteerApps(ctx, email)
		volDur = time.Since(s)
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		regs, regErr = getters.ListRegistrationsByEmail(ctx, email)
		regDur = time.Since(s)
	}()
	go func() {
		defer topWg.Done()
		s := time.Now()
		satEvents, satErr = getters.ListSatelliteEventsBySubmitter(ctx, email)
		satDur = time.Since(s)
	}()
	topWg.Wait()
	ctx.Infos.Printf("/dashboard id=%s fetch wall=%s (sc=%s vol=%s reg=%s sat=%s) → speakers=%d speakerConfs=%d volapps=%d regs=%d satellites=%d",
		reqID, time.Since(t1), scDur, volDur, regDur, satDur, len(speakers), len(speakerConfs), len(volapps), len(regs), len(satEvents))
	if regErr != nil {
		ctx.Err.Printf("/dashboard listregs failed (continuing): %s", regErr)
	}
	if satErr != nil {
		ctx.Err.Printf("/dashboard satellite events failed (continuing): %s", satErr)
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
	attachRegistrationQRCodes(ctx, regs)

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
	ctx.Infos.Printf("/dashboard id=%s listConfs=%s", reqID, time.Since(tConfs))

	t3 := time.Now()
	enrichDashboardProposals(ctx, speakerConfs)
	ctx.Infos.Printf("/dashboard id=%s enrich-proposals=%s", reqID, time.Since(t3))

	activeSC, pastSC := splitSpeakerConfsByEnded(speakerConfs)
	activeVol, pastVol := splitVolAppsByEnded(volapps)
	ctx.Infos.Printf("/dashboard id=%s split activeSC=%d pastSC=%d activeVol=%d pastVol=%d",
		reqID, len(activeSC), len(pastSC), len(activeVol), len(pastVol))
	eligible := eligibleApplyConfs(confs, speakerConfs)
	buyable := buyableConfs(confs)
	tickets := upcomingTickets(regs, confs)

	activeBlocks, pastBlocks := buildEventBlocks(speakerConfs, volapps, tickets, regs, confs, volInfosByConf)
	activeBlocks, pastBlocks = attachSatelliteEventBlocks(activeBlocks, pastBlocks, satEvents, confs)
	// Discover sections at the bottom of the page list confs the user
	// has *no* existing relationship with. Anything already showing as
	// an event block is filtered out so we don't list it twice.
	eligible = excludeConfsInBlocks(eligible, activeBlocks)
	buyable = excludeConfsInBlocks(buyable, activeBlocks)
	ctx.Infos.Printf("/dashboard id=%s blocks active=%d past=%d eligible=%d buyable=%d",
		reqID, len(activeBlocks), len(pastBlocks), len(eligible), len(buyable))

	tRender := time.Now()
	var topSpeaker *types.Speaker
	if len(speakers) > 0 {
		topSpeaker = speakers[0]
	}
	hasPublicProfile := hasPublicWhoIsProfile(ctx, topSpeaker)
	archiveOwnerPath := ""
	if hasPublicProfile && topSpeaker != nil {
		archiveOwnerPath = "/whois/" + publicSpeakerSlug(topSpeaker) + "/archive"
	}
	// Decorate event blocks with the user's admin role for each conf
	// — drives the "Admin" / "Vol coord" link on conf cards. Union
	// the Roles tags across every Speaker row that matches this
	// email, not just speakers[0]: duplicate Speaker pages are common
	// (a person reapplies under a fresh row before an admin merges)
	// and the role tags often live on only one of the pages, so
	// picking just the first row silently drops admin/staff/volcoord
	// access. Dedupe by raw tag to keep ParseRoles' output stable.
	rawRoles := map[string]bool{}
	for _, sp := range speakers {
		if sp == nil {
			continue
		}
		for _, raw := range sp.Roles {
			rawRoles[raw] = true
		}
	}
	rawList := make([]string, 0, len(rawRoles))
	for r := range rawRoles {
		rawList = append(rawList, r)
	}
	idRoles := auth.ParseRoles(rawList)
	id := &auth.Identity{Speaker: topSpeaker, Roles: idRoles}
	if topSpeaker != nil {
		id.Email = topSpeaker.Email
	}
	attachDashboardAdminRoles(activeBlocks, id)
	attachDashboardAdminRoles(pastBlocks, id)
	// Synthesize event blocks for confs the user can admin but has no
	// other relationship with (the global-admin case, or per-conf
	// admins watching events they're not personally speaking at).
	// Without this, an admin's dashboard would surface no Admin
	// button at all because activeBlocks is built only from
	// SpeakerConf/VolApp/Ticket relationships.
	if len(idRoles) > 0 {
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
	// Single Notion call (empty tag = all rows), bucket by tag→day,
	// then shallow-copy each block's Conf so the cached pointer
	// shared with other readers stays untouched.
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

	err = ctx.TemplateCache.ExecuteTemplate(w, "dashboard.tmpl", &DashboardPage{
		Name:             name,
		Hometown:         hometown,
		Photo:            photo,
		Email:            encodedEmail,
		HMAC:             encodedHMAC,
		Speaker:          topSpeaker,
		HasPublicProfile: hasPublicProfile,
		ArchiveOwnerPath: archiveOwnerPath,
		SpeakerConfs:     activeSC,
		PastSpeakerConfs: pastSC,
		VolApps:          activeVol,
		PastVolApps:      pastVol,
		VolInfos:         volInfosByConf,
		Stats:            stats,
		Confs:            confs,
		EligibleConfs:    eligible,
		BuyableConfs:     buyable,
		DiscoverConfs:    discoverConfs(confs, activeBlocks),
		Tickets:          tickets,
		ActiveBlocks:     activeBlocks,
		PastBlocks:       pastBlocks,
		HasUpcomingTalk:  hasUpTalk,
		HasUpcomingVol:   hasUpVol,
		FlashMessage:     r.URL.Query().Get("flash"),
		FlashError:       r.URL.Query().Get("error"),
		IsGlobalAdmin:    id.IsGlobalAdmin(),
		HasAnyTicket:     len(regs) > 0,
		AffiliateCode:    loadAffiliateCode(ctx, email, len(regs) > 0),
		AffiliateStats:   loadAffiliateStats(ctx, email, len(regs) > 0),
		BaseURI:          ctx.Env.GetURI(),
		Year:             helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/dashboard ExecuteTemplate failed: %s", err)
		return
	}
	ctx.Infos.Printf("/dashboard id=%s render=%s", reqID, time.Since(tRender))
}

func renderDashboardLogin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_login.tmpl", &DashboardPage{
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/dashboard render login failed: %s", err)
	}
}

func hasPublicWhoIsProfile(ctx *config.AppContext, speaker *types.Speaker) bool {
	if speaker == nil || speaker.ID == "" {
		return false
	}
	people, err := buildWhoIsDirectory(ctx)
	if err != nil {
		ctx.Err.Printf("/dashboard whois profile check failed: %s", err)
		return false
	}
	for _, person := range people {
		if person != nil && person.Speaker != nil && person.Speaker.ID == speaker.ID {
			return true
		}
	}
	return false
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
// Two-phase to keep everything parallel: first fan-out fetch every unique
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

	// Phase 1: parallel-fetch every unique co-speaker SpeakerConf.
	t1 := time.Now()
	if len(uniqueRefs) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for ref := range uniqueRefs {
			wg.Add(1)
			go func(ref string) {
				defer wg.Done()
				sc, err := getters.FetchSpeakerConfWithSpeaker(ctx, ref)
				if err != nil {
					ctx.Err.Printf("enrich: fetch sc %s: %s", ref, err)
					return
				}
				mu.Lock()
				scCache[ref] = sc
				mu.Unlock()
			}(ref)
		}
		wg.Wait()
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
		if eb.Conf != nil && eb.Conf.HasEnded() {
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
			if conf.HasEnded() {
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

// upcomingTickets joins the user's PurchasesDb rows with the confs cache
// and keeps only those whose conf hasn't ended. Past tickets are dropped
// — no value in offering a PDF for a conf that's already over.
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
		if c == nil || c.HasEnded() {
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

// splitSpeakerConfsByEnded partitions speaker confs by whether their conf
// has ended (per Conf.EndDate). A SpeakerConf with no resolvable conf
// (no proposals or proposals without ScheduleFor) lands in the active
// bucket so it's still visible — better to show too much than to bury it.
func splitSpeakerConfsByEnded(scs []*types.SpeakerConf) (active, past []*types.SpeakerConf) {
	for _, sc := range scs {
		conf := speakerConfConf(sc)
		if conf != nil && conf.HasEnded() {
			past = append(past, sc)
		} else {
			active = append(active, sc)
		}
	}
	return active, past
}

func splitVolAppsByEnded(vols []*types.Volunteer) (active, past []*types.Volunteer) {
	for _, v := range vols {
		if len(v.ScheduleFor) > 0 && v.ScheduleFor[0] != nil && v.ScheduleFor[0].HasEnded() {
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
func loadAffiliateCode(ctx *config.AppContext, email string, eligible bool) *types.DiscountCode {
	if !eligible || email == "" {
		return nil
	}
	code, err := getters.FindAffiliateCodeByEmail(ctx, email)
	if err != nil {
		ctx.Err.Printf("/dashboard affiliate lookup %s: %s", email, err)
		return nil
	}
	return code
}

// loadAffiliateStats sums every AffiliateUsage row for the user via
// a live Notion query (no cache, since affiliates expect to see
// their freshest stats on refresh). Returns zeros when the gate is
// closed; the template renders zeros as "0 tickets sold / $0".
func loadAffiliateStats(ctx *config.AppContext, email string, eligible bool) *AffiliateStats {
	if !eligible || email == "" {
		return &AffiliateStats{}
	}
	totals, err := getters.SumAffiliateStatsByEmail(ctx, email)
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
