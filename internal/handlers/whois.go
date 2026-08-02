package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func RenderWhoIs(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	people, err := buildWhoIsDirectory(ctx)
	if err != nil {
		http.Error(w, "Unable to load speaker directory, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/whois build directory failed: %s", err)
		return
	}
	allCount := len(people)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	topic := slugifyPublicID(r.URL.Query().Get("topic"))
	event := slugifyPublicID(r.URL.Query().Get("event"))
	eventOptions := whoIsEventOptions(people)
	if query != "" || topic != "" || event != "" {
		people = filterWhoIsPeople(people, query, topic, event)
	}
	talkCount, projectCount, editionCount := whoIsTotals(people)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "whois.tmpl", &WhoIsPage{
		People:       people,
		AllCount:     allCount,
		TalkCount:    talkCount,
		ProjectCount: projectCount,
		EditionCount: editionCount,
		Query:        query,
		Topic:        topic,
		Event:        event,
		EventOptions: eventOptions,
		Year:         helpers.CurrentYear(),
	}); err != nil {
		http.Error(w, "Unable to load speaker directory, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/whois ExecuteTemplate failed: %s", err.Error())
		return
	}
}

func RenderWhoIsProfile(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	slug := strings.TrimSpace(mux.Vars(r)["speaker"])
	if slug == "" {
		handle404(w, r, ctx)
		return
	}
	person, err := findWhoIsPerson(ctx, slug)
	if err != nil {
		http.Error(w, "Unable to load speaker profile, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/whois/%s build directory failed: %s", slug, err)
		return
	}
	if person == nil {
		handle404(w, r, ctx)
		return
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "whois_profile.tmpl", &WhoIsProfilePage{
		Person:           person,
		UpdateProfileURL: whoIsProfileEditURL(ctx, r, person),
		Year:             helpers.CurrentYear(),
	}); err != nil {
		http.Error(w, "Unable to load speaker profile, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/whois/%s ExecuteTemplate failed: %s", slug, err.Error())
	}
}

func RenderWhoIsArchive(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	slug := strings.TrimSpace(mux.Vars(r)["speaker"])
	if slug == "" {
		handle404(w, r, ctx)
		return
	}
	person, err := findWhoIsPerson(ctx, slug)
	if err != nil {
		http.Error(w, "Unable to load speaker archive, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/whois/%s/archive build directory failed: %s", slug, err)
		return
	}
	if person == nil {
		handle404(w, r, ctx)
		return
	}

	pastBlocks := whoIsArchiveBlocks(person)
	archiveYears, archiveSessions := dashboardArchiveYears(pastBlocks)
	photo := ""
	name := ""
	if person.Speaker != nil {
		photo = person.Speaker.Photo
		name = person.Speaker.Name
	}
	var encodedEmail, encodedHMAC string
	canEdit := false
	if person.Speaker != nil && person.Speaker.Email != "" {
		email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
		if email != "" && strings.EqualFold(email, person.Speaker.Email) {
			encodedEmail = base64.RawURLEncoding.EncodeToString([]byte(email))
			encodedHMAC = base64.RawURLEncoding.EncodeToString([]byte(helpers.CreateEmailHMAC(ctx, email)))
			canEdit = true
		}
	}
	page := &DashboardPage{
		Name:             name,
		Photo:            photo,
		Email:            encodedEmail,
		HMAC:             encodedHMAC,
		Speaker:          person.Speaker,
		PastBlocks:       pastBlocks,
		ArchiveYears:     archiveYears,
		ArchiveSessions:  archiveSessions,
		ArchiveOwnerName: name,
		ArchiveOwnerPath: "/whois/" + person.PublicID,
		ArchiveIsPublic:  true,
		CanEditArchive:   canEdit,
		FlashMessage:     r.URL.Query().Get("flash"),
		FlashError:       r.URL.Query().Get("error"),
		BaseURI:          ctx.Env.GetURI(),
		Year:             helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_archive.tmpl", page); err != nil {
		http.Error(w, "Unable to load speaker archive, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/whois/%s/archive ExecuteTemplate failed: %s", slug, err)
	}
}

func findWhoIsPerson(ctx *config.AppContext, slug string) (*WhoIsPerson, error) {
	people, err := buildWhoIsDirectory(ctx)
	if err != nil {
		return nil, err
	}
	for _, person := range people {
		if person != nil && person.PublicID == slug {
			return person, nil
		}
	}
	return nil, nil
}

func whoIsArchiveBlocks(person *WhoIsPerson) []*EventBlock {
	if person == nil {
		return nil
	}
	byTag := map[string]*EventBlock{}
	block := func(conf *types.Conf) *EventBlock {
		if conf == nil || conf.Tag == "" {
			return nil
		}
		if existing := byTag[conf.Tag]; existing != nil {
			return existing
		}
		eb := &EventBlock{Conf: conf}
		byTag[conf.Tag] = eb
		return eb
	}
	for _, conf := range person.Editions {
		block(conf)
	}
	for _, row := range person.Talks {
		if row == nil || row.Talk == nil || row.Conf == nil {
			continue
		}
		eb := block(row.Conf)
		if eb == nil {
			continue
		}
		if eb.SpeakerConf == nil {
			eb.SpeakerConf = &types.SpeakerConf{Speaker: person.Speaker}
		}
		proposal := &types.Proposal{
			ID:              row.Talk.ID,
			Title:           row.Talk.Name,
			Description:     row.Talk.Description,
			TalkType:        row.Talk.Type,
			Status:          row.Talk.Status,
			ScheduleFor:     row.Conf,
			DesiredDuration: talkDurationMinutes(row.Talk),
			ConfTalk: &types.ConfTalk{
				ID:            row.Talk.ID,
				Conf:          row.Conf,
				Clipart:       row.Talk.Clipart,
				Sched:         row.Talk.Sched,
				Venue:         row.Talk.Venue,
				GithubRepoURL: row.Talk.GithubRepoURL,
				SlidesURL:     row.Talk.SlidesURL,
			},
		}
		if row.Talk.YTLink != "" {
			proposal.Recording = &types.Recording{YTLink: row.Talk.YTLink}
		}
		if proposal.Status == "" {
			proposal.Status = StatusAccepted
		}
		eb.SpeakerConf.Proposals = append(eb.SpeakerConf.Proposals, proposal)
	}

	blocks := make([]*EventBlock, 0, len(byTag))
	for _, eb := range byTag {
		if eb == nil || eb.Conf == nil {
			continue
		}
		if eb.SpeakerConf == nil {
			eb.Tickets = []*types.Registration{{ConfRef: eb.Conf.Ref, Type: types.TicketTypeGeneral}}
		}
		blocks = append(blocks, eb)
	}
	sort.SliceStable(blocks, func(i, j int) bool {
		return blocks[i].Conf.StartDate.After(blocks[j].Conf.StartDate)
	})
	return blocks
}

func talkDurationMinutes(talk *types.Talk) int {
	if talk == nil || talk.Sched == nil || talk.Sched.End == nil {
		return 0
	}
	return int(talk.Sched.End.Sub(talk.Sched.Start).Minutes())
}

func whoIsProfileEditURL(ctx *config.AppContext, r *http.Request, person *WhoIsPerson) string {
	if ctx == nil || r == nil || person == nil || person.Speaker == nil {
		return ""
	}
	email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
	if email == "" || !strings.EqualFold(email, person.Speaker.Email) {
		return ""
	}
	encodedEmail := base64.RawURLEncoding.EncodeToString([]byte(email))
	encodedHMAC := base64.RawURLEncoding.EncodeToString([]byte(helpers.CreateEmailHMAC(ctx, email)))
	return "/dashboard/speaker?hr=" + url.QueryEscape(encodedHMAC) + "&em=" + url.QueryEscape(encodedEmail)
}

func filterWhoIsPeople(people []*WhoIsPerson, query, topic, event string) []*WhoIsPerson {
	needle := strings.ToLower(strings.TrimSpace(query))
	topic = strings.ToLower(strings.TrimSpace(topic))
	event = strings.ToLower(strings.TrimSpace(event))
	if needle == "" && topic == "" && event == "" {
		return people
	}
	var filtered []*WhoIsPerson
	for _, person := range people {
		if whoIsPersonMatches(person, needle, topic, event) {
			filtered = append(filtered, person)
		}
	}
	return filtered
}

func whoIsPersonMatches(person *WhoIsPerson, needle, topic, event string) bool {
	if person == nil || person.Speaker == nil {
		return false
	}
	s := person.Speaker
	hay := []string{
		person.PublicID,
		s.Name,
		s.Company,
		s.Bio,
		s.Twitter.Handle,
		s.Github,
		s.Website,
	}
	for _, talk := range person.Talks {
		if talk == nil {
			continue
		}
		if talk.Talk != nil {
			hay = append(hay, talk.Talk.Name, talk.Talk.Description)
		}
		if talk.Conf != nil {
			hay = append(hay, talk.Conf.Tag, talk.Conf.Desc, talk.Conf.Location, talk.Conf.DateDesc)
		}
	}
	for _, project := range person.Projects {
		if project == nil || project.Project == nil {
			continue
		}
		hay = append(hay,
			project.Project.Title,
			project.Project.ShortDescription,
			project.Project.Description,
			strings.Join(project.Project.Tags, " "),
		)
		if project.Conf != nil {
			hay = append(hay, project.Conf.Tag, project.Conf.Desc, project.Conf.Location, project.Conf.DateDesc)
		}
	}
	haystack := strings.ToLower(strings.Join(hay, " "))
	if needle != "" && !strings.Contains(haystack, needle) {
		return false
	}
	if topic != "" && !strings.Contains(haystack, topic) {
		return false
	}
	if event != "" {
		if !whoIsPersonHasEvent(person, event) {
			return false
		}
	}
	return true
}

func whoIsPersonHasEvent(person *WhoIsPerson, event string) bool {
	if person == nil || event == "" {
		return false
	}
	for _, edition := range person.Editions {
		if edition != nil && strings.EqualFold(edition.Tag, event) {
			return true
		}
	}
	for _, talk := range person.Talks {
		if talk == nil || talk.Conf == nil {
			continue
		}
		if strings.EqualFold(talk.Conf.Tag, event) {
			return true
		}
	}
	for _, project := range person.Projects {
		if project != nil && project.Conf != nil && strings.EqualFold(project.Conf.Tag, event) {
			return true
		}
	}
	return false
}

func whoIsEventOptions(people []*WhoIsPerson) []*types.Conf {
	byTag := map[string]*types.Conf{}
	for _, person := range people {
		if person == nil {
			continue
		}
		for _, edition := range person.Editions {
			if edition != nil && edition.Tag != "" {
				byTag[edition.Tag] = edition
			}
		}
		for _, talk := range person.Talks {
			if talk == nil || talk.Conf == nil || talk.Conf.Tag == "" {
				continue
			}
			byTag[talk.Conf.Tag] = talk.Conf
		}
		for _, project := range person.Projects {
			if project != nil && project.Conf != nil && project.Conf.Tag != "" {
				byTag[project.Conf.Tag] = project.Conf
			}
		}
	}
	out := make([]*types.Conf, 0, len(byTag))
	for _, conf := range byTag {
		out = append(out, conf)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.StartDate.IsZero() && !b.StartDate.IsZero() && !a.StartDate.Equal(b.StartDate) {
			return a.StartDate.After(b.StartDate)
		}
		return a.Tag > b.Tag
	})
	return out
}

func whoIsTotals(people []*WhoIsPerson) (int, int, int) {
	talks := map[string]bool{}
	projects := map[string]bool{}
	editions := map[string]bool{}
	for _, person := range people {
		if person == nil {
			continue
		}
		for _, edition := range person.Editions {
			if edition == nil || edition.Tag == "" {
				continue
			}
			editions[edition.Tag] = true
		}
		for _, talk := range person.Talks {
			if talk != nil && talk.Talk != nil && talk.Talk.ID != "" {
				talks[talk.Talk.ID] = true
			}
		}
		for _, project := range person.Projects {
			if project != nil && project.Project != nil && project.Project.ID != "" {
				projects[project.Project.ID] = true
			}
		}
	}
	return len(talks), len(projects), len(editions)
}

func buildWhoIsDirectory(ctx *config.AppContext) ([]*WhoIsPerson, error) {
	if ctx == nil {
		return nil, fmt.Errorf("application context is not configured")
	}
	ttl := time.Minute
	if ctx.Env != nil && ctx.Env.CacheTTLSec > 0 {
		ttl = time.Duration(ctx.Env.CacheTTLSec) * time.Second
	}

	// Hold the lock while refreshing so a crawler burst produces one database
	// refresh rather than many identical set-based refreshes.
	whoIsCache.Lock()
	defer whoIsCache.Unlock()
	if whoIsCache.app == ctx && time.Now().Before(whoIsCache.expires) {
		return whoIsCache.people, nil
	}

	profiles, err := getters.ListPublicProfiles(ctx)
	if err != nil {
		// Public profiles are archival data. During a brief database incident,
		// serving the last good snapshot is better than turning every crawler
		// and visitor into another pool waiter.
		if whoIsCache.app == ctx && whoIsCache.people != nil {
			if ctx.Err != nil {
				ctx.Err.Printf("/whois refresh failed; serving stale cache: %s", err)
			}
			whoIsCache.expires = time.Now().Add(30 * time.Second)
			return whoIsCache.people, nil
		}
		return nil, err
	}
	people := make([]*WhoIsPerson, 0, len(profiles))
	for _, profile := range profiles {
		if profile == nil || profile.Speaker == nil {
			continue
		}
		person := &WhoIsPerson{
			Speaker:  profile.Speaker,
			Editions: profile.Editions,
			Talks:    make([]*WhoIsTalk, 0, len(profile.Talks)),
			Projects: make([]*WhoIsProject, 0, len(profile.Projects)),
		}
		for _, row := range profile.Talks {
			if row != nil && row.Talk != nil {
				person.Talks = append(person.Talks, &WhoIsTalk{Talk: row.Talk, Conf: row.Conf})
			}
		}
		for _, row := range profile.Projects {
			if row == nil || row.Project == nil || row.Conf == nil {
				continue
			}
			project := &WhoIsProject{
				Project: row.Project,
				Conf:    row.Conf,
				Awards:  row.Awards,
				URL:     "/" + row.Conf.Tag + "/hackathon/projects/" + row.Project.ID,
				Members: make([]*WhoIsProjectMember, 0, len(row.Members)),
			}
			for _, member := range row.Members {
				if member != nil {
					project.Members = append(project.Members, &WhoIsProjectMember{Member: member})
				}
			}
			person.Projects = append(person.Projects, project)
		}
		sortWhoIsTalks(person.Talks)
		people = append(people, person)
	}
	sort.Slice(people, func(i, j int) bool {
		a := strings.ToLower(people[i].Speaker.Name)
		b := strings.ToLower(people[j].Speaker.Name)
		if a == b {
			return people[i].Speaker.ID < people[j].Speaker.ID
		}
		return a < b
	})
	assignWhoIsPublicIDs(people)
	assignWhoIsProjectMemberPublicIDs(people)
	whoIsCache.app = ctx
	whoIsCache.people = people
	whoIsCache.expires = time.Now().Add(ttl)
	return people, nil
}

func assignWhoIsProjectMemberPublicIDs(people []*WhoIsPerson) {
	publicIDs := make(map[string]string, len(people))
	for _, person := range people {
		if person != nil && person.Speaker != nil {
			publicIDs[person.Speaker.ID] = person.PublicID
		}
	}
	for _, person := range people {
		if person == nil {
			continue
		}
		for _, project := range person.Projects {
			if project == nil {
				continue
			}
			for _, member := range project.Members {
				if member != nil && member.Member != nil {
					member.PublicID = publicIDs[member.Member.PersonID]
				}
			}
		}
	}
}

func invalidateWhoIsDirectoryCache() {
	whoIsCache.Lock()
	whoIsCache.expires = time.Time{}
	whoIsCache.Unlock()
}

func sortWhoIsTalks(talks []*WhoIsTalk) {
	sort.Slice(talks, func(i, j int) bool {
		a, b := talks[i], talks[j]
		at, aok := whoIsTalkTime(a)
		bt, bok := whoIsTalkTime(b)
		if aok && bok && !at.Equal(bt) {
			return at.After(bt)
		}
		if aok != bok {
			return aok
		}
		an, bn := "", ""
		if a != nil && a.Talk != nil {
			an = strings.ToLower(a.Talk.Name)
		}
		if b != nil && b.Talk != nil {
			bn = strings.ToLower(b.Talk.Name)
		}
		return an < bn
	})
}

func whoIsTalkTime(row *WhoIsTalk) (time.Time, bool) {
	if row != nil && row.Talk != nil && row.Talk.Sched != nil {
		return row.Talk.Sched.Start, true
	}
	if row != nil && row.Conf != nil && !row.Conf.StartDate.IsZero() {
		return row.Conf.StartDate, true
	}
	return time.Time{}, false
}

func assignWhoIsPublicIDs(people []*WhoIsPerson) {
	used := map[string]bool{}
	for _, person := range people {
		if person == nil || person.Speaker == nil {
			continue
		}
		base := publicSpeakerSlug(person.Speaker)
		slug := base
		if used[slug] {
			suffix := strings.ReplaceAll(person.Speaker.ID, "-", "")
			if len(suffix) > 8 {
				suffix = suffix[:8]
			}
			slug = strings.Trim(base+"-"+suffix, "-")
			for n := 2; used[slug]; n++ {
				slug = fmt.Sprintf("%s-%d", strings.Trim(base, "-"), n)
			}
		}
		used[slug] = true
		person.PublicID = slug
	}
}

func publicSpeakerSlug(speaker *types.Speaker) string {
	if speaker == nil {
		return "speaker"
	}
	for _, raw := range []string{
		profileHandleForSlug(speaker.Github, "github.com"),
		speaker.Twitter.Handle,
		speaker.Name,
	} {
		if slug := slugifyPublicID(raw); slug != "" {
			return slug
		}
	}
	id := strings.ReplaceAll(speaker.ID, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	if id == "" {
		return "speaker"
	}
	return "speaker-" + id
}

func profileHandleForSlug(raw string, host string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimPrefix(raw, "@")
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	if strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") {
		u, err := url.Parse(raw)
		if err == nil {
			uHost := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
			if uHost == host {
				parts := strings.Split(strings.Trim(u.Path, "/"), "/")
				if len(parts) > 0 {
					raw = parts[0]
				}
			}
		}
	} else {
		lower := strings.TrimPrefix(strings.ToLower(raw), "www.")
		if strings.HasPrefix(lower, host+"/") {
			raw = raw[len(host)+1:]
		}
	}
	raw = strings.Trim(raw, " /")
	if idx := strings.IndexAny(raw, "/?#"); idx >= 0 {
		raw = raw[:idx]
	}
	return raw
}

func slugifyPublicID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// acceptedSpeakersForConf returns the deduped speaker list for the
// conf page's "Who's Coming" section. Unions two sources so the list
// is complete even when one side is sparsely populated:
//
//  1. Speakers attached to ConfTalk-backed Talks for this conf with
//     Proposal.Status == Accepted/Scheduled. This is the source the
//     previous filterSpeakers used and remains the primary feed for
//     events whose Proposal rows don't have ScheduleFor set
//     (older confs / hand-entered ConfTalks pre-dating the
//     proposal-flow).
//
//  2. Accepted/Scheduled Proposals whose ScheduleFor.Tag matches
//     this conf. Picks up speakers attached to a freshly-Accepted
//     proposal whose ConfTalk hasn't been provisioned yet (accept
//     pipeline failure, status flipped manually in Notion, etc).
//
// Any status other than Accepted/Scheduled is filtered out (Applied
// / InReview / Waitlisted / Invited / WeDecline / TheyDecline /
// Rejected). Speaker views get Company / OrgLogo overlaid from the
// per-conf SpeakerConf row so templates referencing Speaker.Company
// render the conf-specific affiliation rather than the stale top-
// level Speaker.Company.
func acceptedSpeakersForConf(ctx *config.AppContext, conf *types.Conf, talks []*types.Talk) types.Speakers {
	var speakers types.Speakers
	seen := make(map[string]bool)
	if conf == nil {
		return speakers
	}

	// Source 1: speakers from ConfTalk-backed Talks (the existing
	// pipeline). Talks already carry conf-overlaid Speaker views
	// from talkFromConfTalk, so we just dedupe and append.
	for _, talk := range talks {
		if talk == nil {
			continue
		}
		if talk.Status != StatusAccepted && talk.Status != "Scheduled" {
			continue
		}
		for _, sp := range talk.Speakers {
			if sp == nil || seen[sp.ID] {
				continue
			}
			seen[sp.ID] = true
			speakers = append(speakers, sp)
		}
	}

	// Source 2: Accepted/Scheduled proposals scheduled for this conf.
	// Best-effort — if the scoped proposal read errors we still
	// return the talks-derived list rather than blanking the page.
	//
	// Proposals only have SpeakerConfRefs (raw page IDs) — the
	// Speakers []*SpeakerConf slice is populated only by callers that
	// run resolveProposalSpeakers (e.g. LoadTalksFromConfTalks). Walk
	// the refs directly via the SpeakerConf cache so this works on
	// proposals that haven't been provisioned a ConfTalk yet (which
	// is exactly the case this source is meant to catch).
	proposals, err := getters.ListProposalsForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("acceptedSpeakersForConf %s proposals: %s", conf.Tag, err)
		return speakers
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	var speakerConfIDs []string
	for _, p := range proposals {
		if p == nil {
			continue
		}
		if p.Status != StatusAccepted && p.Status != "Scheduled" {
			continue
		}
		proposalMap[p.ID] = p
		speakerConfIDs = append(speakerConfIDs, p.SpeakerConfRefs...)
	}
	allSpeakers, err := getters.ListSpeakers(ctx)
	if err != nil {
		ctx.Err.Printf("acceptedSpeakersForConf %s speakers: %s", conf.Tag, err)
		return speakers
	}
	speakerMap := make(map[string]*types.Speaker, len(allSpeakers))
	for _, speaker := range allSpeakers {
		if speaker != nil {
			speakerMap[speaker.ID] = speaker
		}
	}
	speakerConfs, err := getters.ListSpeakerConfsByIDs(ctx, speakerConfIDs, speakerMap, proposalMap)
	if err != nil {
		ctx.Err.Printf("acceptedSpeakersForConf %s speakerconfs: %s", conf.Tag, err)
		return speakers
	}
	for _, sc := range speakerConfs {
		if sc == nil || sc.Speaker == nil {
			continue
		}
		for _, p := range sc.Proposals {
			if p == nil || p.ScheduleFor == nil || p.ScheduleFor.Tag != conf.Tag {
				continue
			}
			if seen[sc.Speaker.ID] {
				break
			}
			seen[sc.Speaker.ID] = true
			view := *sc.Speaker
			view.Company = sc.Company
			view.OrgLogo = sc.OrgPhoto
			speakers = append(speakers, &view)
			break
		}
	}
	return speakers
}

type featuredSpeakerCandidate struct {
	rank    int
	speaker *types.Speaker
}

func splitFeaturedSpeakersForConf(ctx *config.AppContext, conf *types.Conf, speakers types.Speakers) ([]*types.Speaker, []*types.Speaker) {
	if conf == nil {
		return splitFeaturedSpeakersFallback(speakers)
	}
	confTag := conf.Tag
	speakerByID := make(map[string]*types.Speaker, len(speakers))
	for _, speaker := range speakers {
		if speaker != nil {
			speakerByID[speaker.ID] = speaker
		}
	}

	proposals, err := getters.ListProposalsForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("splitFeaturedSpeakersForConf %s proposals: %s", confTag, err)
		return splitFeaturedSpeakersFallback(speakers)
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	var speakerConfIDs []string
	for _, proposal := range proposals {
		if proposal != nil {
			proposalMap[proposal.ID] = proposal
			speakerConfIDs = append(speakerConfIDs, proposal.SpeakerConfRefs...)
		}
	}
	speakerConfs, err := getters.ListSpeakerConfsByIDs(ctx, speakerConfIDs, speakerByID, proposalMap)
	if err != nil {
		ctx.Err.Printf("splitFeaturedSpeakersForConf %s speakerconfs: %s", confTag, err)
		return splitFeaturedSpeakersFallback(speakers)
	}
	speakerConfByID := make(map[string]*types.SpeakerConf, len(speakerConfs))
	for _, speakerConf := range speakerConfs {
		if speakerConf != nil {
			speakerConfByID[speakerConf.ID] = speakerConf
		}
	}

	seenFeatured := map[string]bool{}
	var candidates []featuredSpeakerCandidate
	for _, proposal := range proposals {
		if proposal == nil {
			continue
		}
		if proposal.Status != StatusAccepted && proposal.Status != StatusScheduled {
			continue
		}
		if proposal.ScheduleFor == nil || proposal.ScheduleFor.Tag != confTag {
			continue
		}
		for _, ref := range proposal.SpeakerConfRefs {
			sc := speakerConfByID[ref]
			if sc == nil || sc.Speaker == nil || sc.FeaturedRank <= 0 || sc.FeaturedRank > 6 {
				continue
			}
			if seenFeatured[sc.Speaker.ID] {
				continue
			}
			base := speakerByID[sc.Speaker.ID]
			if base == nil {
				base = sc.Speaker
			}
			view := *base
			if sc.Company != "" {
				view.Company = sc.Company
			}
			if sc.OrgPhoto != "" {
				view.OrgLogo = sc.OrgPhoto
			}
			view.FeaturedTalkTitle = strings.TrimSpace(proposal.Title)
			seenFeatured[sc.Speaker.ID] = true
			candidates = append(candidates, featuredSpeakerCandidate{
				rank:    sc.FeaturedRank,
				speaker: &view,
			})
		}
	}

	if len(candidates) == 0 {
		return splitFeaturedSpeakersFallback(speakers)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return strings.ToLower(candidates[i].speaker.Name) < strings.ToLower(candidates[j].speaker.Name)
	})
	if len(candidates) > 6 {
		candidates = candidates[:6]
	}

	featuredIDs := map[string]bool{}
	featured := make([]*types.Speaker, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.speaker == nil {
			continue
		}
		featuredIDs[candidate.speaker.ID] = true
		featured = append(featured, candidate.speaker)
	}

	community := make([]*types.Speaker, 0, len(speakers)-len(featured))
	for _, speaker := range speakers {
		if speaker == nil || featuredIDs[speaker.ID] {
			continue
		}
		community = append(community, speaker)
	}
	return featured, community
}

func splitFeaturedSpeakersFallback(speakers types.Speakers) ([]*types.Speaker, []*types.Speaker) {
	const fallbackFeaturedCount = 6
	if len(speakers) <= fallbackFeaturedCount {
		return append([]*types.Speaker(nil), speakers...), nil
	}
	return append([]*types.Speaker(nil), speakers[:fallbackFeaturedCount]...), append([]*types.Speaker(nil), speakers[fallbackFeaturedCount:]...)
}
