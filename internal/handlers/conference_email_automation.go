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
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

const conferenceEmailAutomationInterval = 5 * time.Minute
const conferenceEmailStartupDelay = 5 * time.Second

func StartConferenceEmailAutomation(ctx *config.AppContext) {
	if ctx == nil || ctx.Env == nil || !ctx.InProduction || ctx.Env.MailOff {
		if ctx != nil && ctx.Infos != nil {
			ctx.Infos.Println("conference email automation disabled outside mail-enabled production")
		}
		return
	}
	go func() {
		// Final-attendee sends render ticket PDFs through the local HTTP
		// server. Give ListenAndServe a moment to bind before startup catch-up.
		time.Sleep(conferenceEmailStartupDelay)
		runConferenceEmailAutomation(ctx, time.Now())
		ticker := time.NewTicker(conferenceEmailAutomationInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			runConferenceEmailAutomation(ctx, now)
		}
	}()
}

func runConferenceEmailAutomation(ctx *config.AppContext, now time.Time) {
	if err := getters.ReconcileConferenceEmailCampaigns(ctx, now); err != nil {
		ctx.Err.Printf("conference email reconciliation failed: %s", err)
		return
	}
	builds, err := getters.ClaimConferenceEmailBuilds(ctx, now, 20)
	if err != nil {
		ctx.Err.Printf("conference email build claim failed: %s", err)
		return
	}
	for _, occurrence := range builds {
		if err := buildConferenceEmailDraft(ctx, occurrence); err != nil {
			ctx.Err.Printf("conference email draft %s failed: %s", occurrence.ID, err)
			_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", err)
		}
	}
	runConferenceEmailSendAutomation(ctx, now)
}

func runConferenceEmailSendAutomation(ctx *config.AppContext, now time.Time) {
	sends, err := getters.ClaimConferenceEmailSends(ctx, now, 10)
	if err != nil {
		ctx.Err.Printf("conference email send claim failed: %s", err)
		return
	}
	for _, occurrence := range sends {
		if err := sendConferenceEmailOccurrence(ctx, occurrence); err != nil {
			ctx.Err.Printf("conference email send %s failed: %s", occurrence.ID, err)
			_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", err)
		}
	}
}

func runConferenceEmailDraftAutomationForConference(ctx *config.AppContext, conf *types.Conf, now time.Time) {
	if err := getters.EnsureConferenceEmailCampaigns(ctx, conf, now); err != nil {
		ctx.Err.Printf("conference email reconciliation for %s failed: %s", conf.Tag, err)
		return
	}
	builds, err := getters.ClaimConferenceEmailBuildsForConference(ctx, now, 20, conf.Ref)
	if err != nil {
		ctx.Err.Printf("conference email build claim for %s failed: %s", conf.Tag, err)
		return
	}
	for _, occurrence := range builds {
		if err := buildConferenceEmailDraft(ctx, occurrence); err != nil {
			ctx.Err.Printf("conference email draft %s failed: %s", occurrence.ID, err)
			_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", err)
		}
	}
}

func buildConferenceEmailDraft(ctx *config.AppContext, occurrence *types.ConferenceEmailOccurrence) error {
	return buildConferenceEmailDraftWithReview(ctx, occurrence, true)
}

func buildConferenceEmailDraftWithReview(ctx *config.AppContext, occurrence *types.ConferenceEmailOccurrence, notifyReview bool) error {
	conf, err := getters.GetConfByRef(ctx, occurrence.ConferenceID)
	if err != nil || conf == nil {
		return fmt.Errorf("load conference for draft: %w", err)
	}
	campaigns, err := getters.ListConferenceEmailCampaigns(ctx, conf.Ref)
	if err != nil {
		return err
	}
	var campaign *types.ConferenceEmailCampaign
	for _, candidate := range campaigns {
		if candidate.ID == occurrence.CampaignID {
			campaign = candidate
			break
		}
	}
	if campaign == nil {
		return fmt.Errorf("campaign %s not found", occurrence.CampaignID)
	}
	updates, err := conferenceEmailGeneratedUpdates(ctx, conf, occurrence)
	if err != nil {
		return err
	}
	sponsorFooter := ""
	if conferenceCampaignHasSponsorFooter(occurrence.CampaignKind) {
		sponsorFooter, err = conferenceSponsorsMarkdown(ctx, conf)
		if err != nil {
			return err
		}
	}
	markdown := materializeConferenceDraftMarkdown(campaign.Markdown, updates, sponsorFooter)
	heroURL, err := conferenceEmailHeroURL(ctx, conf)
	if err != nil {
		return err
	}
	markdown = conferenceEmailMarkdownWithHero(markdown, heroURL)
	expiry := conf.EndDate
	var expiryPtr *time.Time
	if !expiry.IsZero() {
		expiryPtr = &expiry
	}
	letter, err := getters.CreateConferenceOccurrenceDraft(ctx, occurrence, types.ConferenceCampaignSubject(campaign.Title), markdown, expiryPtr)
	if err != nil {
		return err
	}
	ctx.Infos.Printf("conference email draft MISS-%d built for %s/%s", letter.UID, conf.Tag, occurrence.CampaignKind)
	if notifyReview {
		if err := emails.SendConferenceCampaignDraftReview(ctx, conf, occurrence, letter); err != nil {
			ctx.Err.Printf("conference email draft MISS-%d review notification failed: %s", letter.UID, err)
		}
	}
	return nil
}

func materializeConferenceDraftMarkdown(markdown, updates, sponsorFooter string) string {
	markdown = strings.ReplaceAll(markdown, "{{ .GeneratedUpdates }}", updates)
	// The acknowledgement is a draft footer, not part of generated updates.
	// Remove its optional source marker before appending it so event-specific
	// edits cannot accidentally leave it in the middle of the email.
	markdown = strings.TrimSpace(strings.ReplaceAll(markdown, "{{ .SponsorAcknowledgement }}", ""))
	if sponsorFooter = strings.TrimSpace(sponsorFooter); sponsorFooter != "" {
		markdown += "\n\n" + sponsorFooter
	}
	return markdown
}

func conferenceCampaignHasSponsorFooter(kind string) bool {
	switch kind {
	case types.ConferenceCampaignAttendeeReminder70,
		types.ConferenceCampaignAttendeeReminder49,
		types.ConferenceCampaignAttendeeReminder28,
		types.ConferenceCampaignAttendeeFinal,
		types.ConferenceCampaignSpeakerReminder:
		return true
	default:
		return false
	}
}

func conferenceEmailHeroURL(ctx *config.AppContext, conf *types.Conf) (string, error) {
	if ctx == nil || ctx.Env == nil || conf == nil {
		return "", fmt.Errorf("conference email hero is not configured")
	}
	baseURI := strings.TrimRight(ctx.Env.GetURI(), "/")
	fallback := baseURI + "/static/img/" + url.PathEscape(conf.Tag) + "/leading.png"
	clipart, err := getters.LatestConferenceTalkClipart(ctx, conf.Ref)
	if err != nil {
		return "", err
	}
	if clipart == "" {
		return fallback, nil
	}
	if parsed, parseErr := url.Parse(clipart); parseErr == nil && parsed.IsAbs() {
		return clipart, nil
	}
	localPath := strings.TrimPrefix(strings.TrimPrefix(clipart, "../"), "/")
	if strings.HasPrefix(localPath, "static/") {
		return baseURI + "/" + localPath, nil
	}
	if spaces.IsConfigured() {
		return spaces.PublicURL("talks/" + strings.TrimPrefix(clipart, "talks/")), nil
	}
	return fallback, nil
}

func conferenceEmailMarkdownWithHero(markdown, heroURL string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	heroURL = strings.TrimSpace(heroURL)
	if heroURL == "" {
		return markdown
	}
	if !strings.HasPrefix(markdown, "---\n") {
		return "---\nhero: " + strconv.Quote(heroURL) + "\n---\n\n" + strings.TrimLeft(markdown, "\n")
	}
	end := strings.Index(markdown[4:], "\n---")
	if end < 0 {
		return markdown
	}
	header := markdown[4 : 4+end]
	lines := strings.Split(header, "\n")
	kept := lines[:0]
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "hero") {
			continue
		}
		kept = append(kept, line)
	}
	header = strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if header != "" {
		header += "\n"
	}
	rest := markdown[4+end+len("\n---"):]
	return "---\n" + header + "hero: " + strconv.Quote(heroURL) + "\n---" + rest
}

func conferenceEmailGeneratedUpdates(ctx *config.AppContext, conf *types.Conf, occurrence *types.ConferenceEmailOccurrence) (string, error) {
	var sections []string
	switch occurrence.CampaignKind {
	case types.ConferenceCampaignAttendeeReminder70,
		types.ConferenceCampaignAttendeeReminder49,
		types.ConferenceCampaignAttendeeReminder28,
		types.ConferenceCampaignAttendeeFinal:
		speakers, err := conferenceSpeakersMarkdown(ctx, conf, occurrence.CampaignKind == types.ConferenceCampaignAttendeeFinal)
		if err != nil {
			return "", err
		}
		sections = append(sections, speakers)
		sections = append(sections, conferenceVenueMarkdown(conf, emails.DoorsOpenDesc(ctx, conf), emails.BreakfastStartDesc(ctx, conf)))
		sections = append(sections, conferenceHotelsMarkdown(ctx, conf))
		if occurrence.CampaignKind == types.ConferenceCampaignAttendeeFinal {
			sections = append(sections, conferenceAgendaMarkdown(ctx, conf))
			hackathon, err := conferenceHackathonMarkdown(ctx, conf)
			if err != nil {
				return "", err
			}
			sections = append(sections, hackathon)
			satellites, err := conferenceSatelliteEventsMarkdown(ctx, conf)
			if err != nil {
				return "", err
			}
			sections = append(sections, satellites)
		}
	case types.ConferenceCampaignSpeakerReminder:
		sections = append(sections, conferenceHotelsMarkdown(ctx, conf))
		if conferenceHasOpenVolunteerShifts(ctx, conf) {
			sections = append(sections, "Volunteer shifts are still available. [Sign up to help]("+ctx.Env.GetURI()+"/"+conf.Tag+"#volunteer).")
		}
	case types.ConferenceCampaignSpeakerOnboarding:
		sections = append(sections, conferenceHotelsMarkdown(ctx, conf))
		sections = append(sections, conferenceVenueMarkdown(conf, emails.DoorsOpenDesc(ctx, conf), emails.BreakfastStartDesc(ctx, conf)))
	case types.ConferenceCampaignVolunteerOrient:
		volInfo, err := getters.GetVolInfo(ctx, conf.Ref)
		if err != nil {
			return "", err
		}
		if volInfo.OrientTimes != nil {
			when := volInfo.OrientTimes.Start.In(conf.Loc()).Format("Monday, January 2 at 3:04 PM MST")
			sections = append(sections, "- **When:** "+when)
		}
		if volInfo.OrientLink != "" {
			sections = append(sections, "- **Where:** [Jitsi meeting link (online)]("+volInfo.OrientLink+")")
		}
		if volInfo.Notes != "" {
			sections = append(sections, volInfo.Notes)
		}
	}
	clean := sections[:0]
	for _, section := range sections {
		if strings.TrimSpace(section) != "" {
			clean = append(clean, section)
		}
	}
	return strings.Join(clean, "\n\n"), nil
}

func conferenceVenueMarkdown(conf *types.Conf, doorsOpen, breakfastStart string) string {
	if conf == nil {
		return ""
	}
	venue := strings.TrimSpace(strings.Join([]string{conf.Venue, conf.Location}, ", "))
	venue = strings.Trim(venue, ", ")
	if venue == "" {
		return ""
	}
	copy := "The event will be held at " + venue
	if dateDesc := strings.TrimSpace(conf.DateDesc); dateDesc != "" {
		copy += " on " + dateDesc
	}
	copy += "."
	if doorsOpen = strings.TrimSpace(doorsOpen); doorsOpen != "" {
		copy += " Doors will open at " + doorsOpen
		if breakfastStart = strings.TrimSpace(breakfastStart); breakfastStart != "" {
			copy += " and coffee will be served at " + breakfastStart
		}
		copy += "."
	}
	if venueMap := strings.TrimSpace(conf.VenueMap); venueMap != "" {
		copy += " [View the venue on Google Maps →](" + venueMap + ")"
	}
	return "### Where to go\n\n" + copy
}

func conferenceHotelsMarkdown(ctx *config.AppContext, conf *types.Conf) string {
	hotels, err := getters.ListHotelsForConf(ctx, conf.Ref)
	if err != nil || len(hotels) == 0 {
		return ""
	}
	return conferenceHotelsListMarkdown(hotels)
}

func conferenceHotelsListMarkdown(hotels []*types.Hotel) string {
	lines := []string{"### Where to stay"}
	for _, hotel := range hotels {
		if hotel == nil || strings.TrimSpace(hotel.Name) == "" {
			continue
		}
		name := strings.TrimSpace(hotel.Name)
		if hotel.URL != "" {
			name = "[" + name + "](" + strings.TrimSpace(hotel.URL) + ")"
		}
		line := "- " + name
		if priceRange := strings.TrimSpace(hotel.PriceRange); priceRange != "" {
			line += " — **Price range:** " + priceRange
		}
		if notes := strings.TrimSpace(hotel.Desc); notes != "" {
			line += "\n  - " + notes
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func conferenceSpeakersMarkdown(ctx *config.AppContext, conf *types.Conf, useFeaturedOrder bool) (string, error) {
	if ctx == nil || ctx.Env == nil || conf == nil {
		return "", nil
	}
	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		return "", fmt.Errorf("query attendee reminder talks: %w", err)
	}
	speakers := acceptedSpeakersForConf(ctx, conf, talks)
	if len(speakers) == 0 {
		return "", nil
	}
	sort.Sort(speakers)
	ordered := speakers
	if useFeaturedOrder {
		featured, community := splitFeaturedSpeakersForConf(ctx, conf, speakers)
		ordered = append(append(types.Speakers{}, featured...), community...)
	} else {
		ordered = recentConferenceSpeakers(ctx, conf, speakers)
	}
	talkTitles, err := conferenceEmailSpeakerTalkTitles(ctx, conf, talks)
	if err != nil {
		return "", err
	}
	return conferenceSpeakerListMarkdown(ctx.Env.GetURI(), conf, ordered, talkTitles), nil
}

func recentConferenceSpeakers(ctx *config.AppContext, conf *types.Conf, speakers types.Speakers) types.Speakers {
	if ctx == nil || conf == nil || len(speakers) == 0 {
		return speakers
	}
	proposals, err := getters.ListProposalsForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("recentConferenceSpeakers %s proposals: %s", conf.Tag, err)
		return speakers
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	var speakerConfIDs []string
	for _, proposal := range proposals {
		if proposal == nil || (proposal.Status != StatusAccepted && proposal.Status != StatusScheduled) {
			continue
		}
		proposalMap[proposal.ID] = proposal
		speakerConfIDs = append(speakerConfIDs, proposal.SpeakerConfRefs...)
	}
	speakerByID := make(map[string]*types.Speaker, len(speakers))
	for _, speaker := range speakers {
		if speaker != nil {
			speakerByID[speaker.ID] = speaker
		}
	}
	speakerConfs, err := getters.ListSpeakerConfsByIDs(ctx, speakerConfIDs, speakerByID, proposalMap)
	if err != nil {
		ctx.Err.Printf("recentConferenceSpeakers %s speaker records: %s", conf.Tag, err)
		return speakers
	}
	acceptedAt := make(map[string]*time.Time, len(speakerConfs))
	for _, speakerConf := range speakerConfs {
		if speakerConf == nil || speakerConf.Speaker == nil || speakerConf.AcceptedAt == nil {
			continue
		}
		existing := acceptedAt[speakerConf.Speaker.ID]
		if existing == nil || speakerConf.AcceptedAt.After(*existing) {
			acceptedAt[speakerConf.Speaker.ID] = speakerConf.AcceptedAt
		}
	}
	return orderSpeakersByAcceptedAt(speakers, acceptedAt)
}

func orderSpeakersByAcceptedAt(speakers types.Speakers, acceptedAt map[string]*time.Time) types.Speakers {
	ordered := append(types.Speakers{}, speakers...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i] == nil || ordered[j] == nil {
			return ordered[j] == nil && ordered[i] != nil
		}
		left := acceptedAt[ordered[i].ID]
		right := acceptedAt[ordered[j].ID]
		switch {
		case left != nil && right != nil && !left.Equal(*right):
			return left.After(*right)
		case left != nil && right == nil:
			return true
		case left == nil && right != nil:
			return false
		default:
			return strings.ToLower(ordered[i].Name) < strings.ToLower(ordered[j].Name)
		}
	})
	return ordered
}

func conferenceEmailSpeakerTalkTitles(ctx *config.AppContext, conf *types.Conf, talks []*types.Talk) (map[string][]string, error) {
	titles := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	add := func(speakerID, title string) {
		speakerID = strings.TrimSpace(speakerID)
		title = strings.TrimSpace(title)
		if speakerID == "" || title == "" {
			return
		}
		if seen[speakerID] == nil {
			seen[speakerID] = make(map[string]bool)
		}
		key := strings.ToLower(title)
		if seen[speakerID][key] {
			return
		}
		seen[speakerID][key] = true
		titles[speakerID] = append(titles[speakerID], title)
	}
	for _, talk := range talks {
		if talk == nil || (talk.Status != StatusAccepted && talk.Status != StatusScheduled) {
			continue
		}
		for _, speaker := range talk.Speakers {
			if speaker != nil {
				add(speaker.ID, talk.Name)
			}
		}
	}

	proposals, err := getters.ListProposalsForConf(ctx, conf.Ref)
	if err != nil {
		return nil, fmt.Errorf("query attendee reminder speaker proposals: %w", err)
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	var speakerConfIDs []string
	for _, proposal := range proposals {
		if proposal == nil || (proposal.Status != StatusAccepted && proposal.Status != StatusScheduled) {
			continue
		}
		proposalMap[proposal.ID] = proposal
		speakerConfIDs = append(speakerConfIDs, proposal.SpeakerConfRefs...)
	}
	speakerConfs, err := getters.ListSpeakerConfsByIDs(ctx, speakerConfIDs, nil, proposalMap)
	if err != nil {
		return nil, fmt.Errorf("query attendee reminder speaker records: %w", err)
	}
	speakerConfByID := make(map[string]*types.SpeakerConf, len(speakerConfs))
	for _, speakerConf := range speakerConfs {
		if speakerConf != nil {
			speakerConfByID[speakerConf.ID] = speakerConf
		}
	}
	for _, proposal := range proposals {
		if proposal == nil || (proposal.Status != StatusAccepted && proposal.Status != StatusScheduled) {
			continue
		}
		for _, speakerConfID := range proposal.SpeakerConfRefs {
			if speakerConf := speakerConfByID[speakerConfID]; speakerConf != nil && speakerConf.Speaker != nil {
				add(speakerConf.Speaker.ID, proposal.Title)
			}
		}
	}
	for speakerID := range titles {
		sort.SliceStable(titles[speakerID], func(i, j int) bool {
			return strings.ToLower(titles[speakerID][i]) < strings.ToLower(titles[speakerID][j])
		})
	}
	return titles, nil
}

func conferenceSpeakerListMarkdown(baseURI string, conf *types.Conf, speakers types.Speakers, talkTitles map[string][]string) string {
	if conf == nil || len(speakers) == 0 {
		return ""
	}
	speakersURL := strings.TrimRight(baseURI, "/") + "/" + url.PathEscape(conf.Tag) + "#speakers"
	noun := "speakers"
	if len(speakers) == 1 {
		noun = "speaker"
	}
	lines := []string{
		"### Who's speaking",
		fmt.Sprintf("We've got %d %s coming to %s. [Find the whole list of speakers on the website →](%s)", len(speakers), noun, conferenceEmailMarkdownText(conf.Desc), speakersURL),
	}
	limit := min(len(speakers), 6)
	for _, speaker := range speakers[:limit] {
		if speaker == nil || strings.TrimSpace(speaker.Name) == "" {
			continue
		}
		parts := []string{"**" + conferenceEmailMarkdownText(speaker.Name) + "**"}
		if company := conferenceEmailMarkdownText(speaker.Company); company != "" {
			parts = append(parts, company)
		}
		if titles := talkTitles[speaker.ID]; len(titles) > 0 {
			label := "Talk"
			if len(titles) > 1 {
				label = "Talks"
			}
			escaped := make([]string, 0, len(titles))
			for _, title := range titles {
				escaped = append(escaped, "*"+conferenceEmailMarkdownText(title)+"*")
			}
			parts = append(parts, label+": "+strings.Join(escaped, "; "))
		}
		if links := conferenceEmailSpeakerLinks(speaker); len(links) > 0 {
			parts = append(parts, strings.Join(links, " "))
		}
		lines = append(lines, "- "+strings.Join(parts, " — "))
	}
	return strings.Join(lines, "\n\n")
}

func conferenceEmailSpeakerLinks(speaker *types.Speaker) []string {
	if speaker == nil {
		return nil
	}
	type link struct{ label, href string }
	links := []link{
		{"website", websiteURL(speaker.Website)},
		{"x.com", speaker.Twitter.Link()},
		{"nostr", profileURL(speaker.Nostr, "njump.me")},
		{"github", profileURL(speaker.Github, "github.com")},
		{"instagram", instagramURL(speaker.Instagram)},
		{"linkedin", profileURL(speaker.LinkedIn, "linkedin.com/in")},
		{"leetcode", profileURL(speaker.LeetCode, "leetcode.com")},
	}
	out := make([]string, 0, len(links))
	for _, item := range links {
		if item.href != "" {
			out = append(out, "["+item.label+"]("+item.href+")")
		}
	}
	return out
}

func conferenceSponsorsMarkdown(ctx *config.AppContext, conf *types.Conf) (string, error) {
	if ctx == nil || conf == nil {
		return "", nil
	}
	sponsorships, err := getters.ListSponsorships(ctx, conf.Ref)
	if err != nil {
		return "", fmt.Errorf("query attendee reminder sponsors: %w", err)
	}
	names := conferenceEmailSponsorNames(sponsorships)
	if len(names) == 0 {
		return "", nil
	}
	return "Big thank you to our sponsors: " + humanList(names) + ".", nil
}

func conferenceEmailSponsorNames(sponsorships []*types.Sponsorship) []string {
	visible := make([]*types.Sponsorship, 0, len(sponsorships))
	for _, sponsorship := range sponsorships {
		if sponsorship == nil || sponsorship.Org == nil || !visibleSponsorStatus(sponsorship.Status) || strings.TrimSpace(sponsorship.Org.Name) == "" {
			continue
		}
		visible = append(visible, sponsorship)
	}
	sort.SliceStable(visible, func(i, j int) bool {
		leftLevel := normalizeLevel(visible[i].Level)
		rightLevel := normalizeLevel(visible[j].Level)
		if leftLevel == "" {
			leftLevel = strings.TrimSpace(visible[i].Level)
		}
		if rightLevel == "" {
			rightLevel = strings.TrimSpace(visible[j].Level)
		}
		leftRank := tierRank(leftLevel)
		rightRank := tierRank(rightLevel)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if leftLevel != rightLevel {
			return strings.ToLower(leftLevel) < strings.ToLower(rightLevel)
		}
		return strings.ToLower(visible[i].Org.Name) < strings.ToLower(visible[j].Org.Name)
	})

	names := make([]string, 0, len(visible))
	seen := make(map[string]bool, len(visible))
	for _, sponsorship := range visible {
		name := strings.TrimSpace(sponsorship.Org.Name)
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		label := conferenceEmailMarkdownText(name)
		if website := strings.TrimSpace(sponsorship.Org.Website); website != "" {
			label = "[" + label + "](" + website + ")"
		}
		names = append(names, label)
	}
	return names
}

func humanList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

func conferenceAgendaMarkdown(ctx *config.AppContext, conf *types.Conf) string {
	if ctx == nil || ctx.Env == nil || conf == nil || strings.TrimSpace(conf.Tag) == "" {
		return ""
	}
	agendaURL := strings.TrimRight(ctx.Env.GetURI(), "/") + "/" + url.PathEscape(conf.Tag) + "#agenda"
	return "### Agenda\n\nTimes, rooms, and schedule updates are on the [conference agenda](" + agendaURL + ")."
}

func conferenceHackathonMarkdown(ctx *config.AppContext, conf *types.Conf) (string, error) {
	if ctx == nil || ctx.Env == nil || conf == nil {
		return "", nil
	}
	competition, err := getters.GetCompetitionByConferenceID(ctx, conf.Ref)
	if err != nil {
		return "", fmt.Errorf("query final attendee hackathon: %w", err)
	}
	if competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic {
		return "", nil
	}

	hackathonURL := strings.TrimRight(ctx.Env.GetURI(), "/") + hackathonURLForConf(conf)
	title := strings.TrimSpace(competition.Title)
	if title == "" {
		title = "Hackathon"
	}
	lines := []string{"### " + conferenceEmailMarkdownText(title)}
	if competition.HackingStartsAt != nil && !competition.HackingStartsAt.IsZero() {
		when := competition.HackingStartsAt.In(conf.Loc()).Format("Monday, January 2 at 3:04 PM MST")
		lines = append(lines, "- **Hacking starts:** "+when)
	}
	if competition.MaxTeamSize != nil {
		lines = append(lines, fmt.Sprintf("- **Teams:** Build locally during the event with up to %d people. Teammates join through their attendee profiles. Every team must have at least one member physically present at the event.", *competition.MaxTeamSize))
	} else {
		lines = append(lines, "- **Teams:** Build locally during the event. Teammates join through their attendee profiles. Every team must have at least one member physically present at the event.")
	}

	prizes, err := getters.ListPrizesForCompetition(ctx, competition.ID)
	if err != nil {
		return "", fmt.Errorf("query final attendee hackathon prizes: %w", err)
	}
	if summary := conferenceHackathonPrizeSummary(prizes); summary != "" {
		lines = append(lines, "- **Prizes:** "+summary)
	}
	lines = append(lines, "- [Hackathon details and full schedule]("+hackathonURL+")")
	return strings.Join(lines, "\n"), nil
}

func conferenceHackathonPrizeSummary(prizes []*types.Prize) string {
	public := make([]*types.Prize, 0, len(prizes))
	for _, prize := range prizes {
		if prize == nil || prize.Status == getters.PrizeStatusNeedsFunds {
			continue
		}
		public = append(public, prize)
	}
	parts := make([]string, 0, 2)
	if sats := cashPrizeValueSatsTotal(public); sats > 0 {
		parts = append(parts, compactSatoshiLabel(sats))
	}
	if names := nonCashPrizeNames(public); len(names) > 0 {
		parts = append(parts, strings.Join(names, ", "))
	}
	return strings.Join(parts, " plus ")
}

func conferenceSatelliteEventsMarkdown(ctx *config.AppContext, conf *types.Conf) (string, error) {
	if ctx == nil || ctx.Env == nil || conf == nil {
		return "", nil
	}
	events, err := getters.ListSatelliteEvents(ctx, conf.Ref, false)
	if err != nil {
		return "", fmt.Errorf("query final attendee satellite events: %w", err)
	}
	if len(events) == 0 {
		return "", nil
	}
	return conferenceSatelliteEventsListMarkdown(ctx.Env.GetURI(), conf, events), nil
}

func conferenceSatelliteEventsListMarkdown(baseURI string, conf *types.Conf, events []*types.SatelliteEvent) string {
	if conf == nil || len(events) == 0 {
		return ""
	}
	conferenceURL := strings.TrimRight(baseURI, "/") + "/" + url.PathEscape(conf.Tag) + "#satellites"
	lines := []string{"### Satellite events", "These events may require separate registration or tickets."}
	for _, event := range events {
		if event == nil || strings.TrimSpace(event.Title) == "" {
			continue
		}
		moreInfoURL := strings.TrimSpace(event.EventURL)
		if moreInfoURL == "" {
			moreInfoURL = conferenceURL
		}
		parts := []string{"**" + conferenceEmailMarkdownText(event.Title) + "**"}
		if when := satelliteEventTimeLabel(event, conf); when != "" {
			parts = append(parts, conferenceEmailMarkdownText(when))
		}
		if description := conferenceEmailMarkdownText(event.Description); description != "" {
			parts = append(parts, description)
		}
		parts = append(parts, "[More info]("+moreInfoURL+")")
		lines = append(lines, "- "+strings.Join(parts, " — "))
	}
	if len(lines) == 2 {
		return ""
	}
	return strings.Join(lines, "\n\n")
}

func conferenceEmailMarkdownText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "[", `\[`)
	value = strings.ReplaceAll(value, "]", `\]`)
	return value
}

func conferenceHasOpenVolunteerShifts(ctx *config.AppContext, conf *types.Conf) bool {
	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		return false
	}
	for _, shift := range shifts {
		if shift != nil && int(shift.MaxVols) > len(shift.AssigneesRef) {
			return true
		}
	}
	return false
}

func sendConferenceEmailOccurrence(ctx *config.AppContext, occurrence *types.ConferenceEmailOccurrence) error {
	conf, err := getters.GetConfByRef(ctx, occurrence.ConferenceID)
	if err != nil || conf == nil {
		return fmt.Errorf("load conference for delivery: %w", err)
	}
	letter, err := getters.GetLetter(ctx, occurrence.MissiveUID)
	if err != nil {
		return fmt.Errorf("load occurrence missive: %w", err)
	}
	recipients, err := getters.ConferenceEmailRecipients(ctx, occurrence)
	if err != nil {
		return err
	}
	var failures []string
	for _, recipient := range recipients {
		jobKey := fmt.Sprintf("conference-email-%s-%s", occurrence.ID, helpers.MakeJobHash(recipient.Email, letter.UID, recipient.Key))
		delivery, alreadyQueued, err := getters.BeginConferenceEmailDelivery(ctx, occurrence.ID, recipient.Key, recipient.Email, jobKey)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if alreadyQueued {
			continue
		}
		data := conferenceCampaignRecipientData(ctx, conf, recipient)
		data.SendAt = occurrence.SendAt
		var files []*emails.EmailFile
		if occurrence.CampaignKind == types.ConferenceCampaignAttendeeFinal {
			for _, registration := range recipient.Registrations {
				pdf, pdfErr := emails.MakeTicketPDF(ctx, registration)
				if pdfErr != nil {
					err = fmt.Errorf("build ticket %s: %w", registration.RefID, pdfErr)
					break
				}
				short := registration.RefID
				if len(short) > 8 {
					short = short[:8]
				}
				files = append(files, &emails.EmailFile{PDF: pdf, Name: fmt.Sprintf("btcpp_%s_ticket_%s.pdf", conf.Tag, short)})
			}
		}
		if err == nil {
			err = emails.SendConferenceCampaign(ctx, letter, data, jobKey, files)
			if err != nil && strings.Contains(err.Error(), "scheduled.idem_key") {
				err = nil
			}
		}
		_ = getters.FinishConferenceEmailDelivery(ctx, delivery.ID, err)
		if err != nil {
			failures = append(failures, recipient.Email+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d conference email deliveries failed: %s", len(failures), strings.Join(failures, "; "))
	}
	return getters.CompleteConferenceEmailOccurrence(ctx, occurrence.ID, time.Now())
}

func conferenceCampaignRecipientData(ctx *config.AppContext, conf *types.Conf, recipient *types.ConferenceEmailRecipient) *emails.ConferenceCampaignData {
	dinnerStart := conf.SpeakerDinnerStart
	if dinnerStart == nil {
		day := conf.StartDate.In(conf.Loc()).AddDate(0, 0, -1)
		fallback := time.Date(day.Year(), day.Month(), day.Day(), 18, 30, 0, 0, conf.Loc())
		dinnerStart = &fallback
	}
	dinnerLocation := strings.TrimSpace(conf.SpeakerDinnerLocation)
	if dinnerLocation == "" {
		dinnerLocation = "Location TBD"
	}
	data := &emails.ConferenceCampaignData{
		Conf: conf, Email: recipient.Email, Name: recipient.Name, URI: ctx.Env.GetURI(),
		DashboardLink:          helpers.EmailLink(ctx, recipient.Email, "/dashboard"),
		AffiliateDashboardLink: helpers.EmailLink(ctx, recipient.Email, "/dashboard/affiliate"),
		DoorsOpen:              emails.DoorsOpenDesc(ctx, conf), BreakfastStart: emails.BreakfastStartDesc(ctx, conf), SpeakerDinnerLocation: dinnerLocation,
	}
	if dinnerStart != nil {
		data.SpeakerDinnerTime = dinnerStart.In(conf.Loc()).Format("Monday, January 2 at 3:04 PM MST")
	}
	if recipient.SpeakerConfID != "" {
		if speakerConf, err := getters.GetSpeakerConfByID(ctx, recipient.SpeakerConfID); err == nil && speakerConf != nil {
			var talks []string
			for _, proposal := range speakerConf.Proposals {
				if proposal == nil || (proposal.Status != StatusAccepted && proposal.Status != StatusScheduled) {
					continue
				}
				if proposal.ScheduleFor != nil && proposal.ScheduleFor.Ref != "" && proposal.ScheduleFor.Ref != conf.Ref {
					continue
				}
				confTalk, confTalkErr := getters.GetConfTalkByProposal(ctx, proposal.ID)
				if confTalkErr != nil {
					ctx.Err.Printf("conference speaker email talk %s: %s", proposal.ID, confTalkErr)
				}
				speakers, speakersErr := getters.ListProposalSpeakerSummaries(ctx, proposal.ID)
				if speakersErr != nil {
					ctx.Err.Printf("conference speaker email co-speakers %s: %s", proposal.ID, speakersErr)
				}
				talks = append(talks, conferenceSpeakerTalkMarkdown(conf, proposal, confTalk, recipient.SpeakerConfID, speakers))
			}
			if len(talks) > 0 {
				data.TalkDetails = "### Your talk\n\nHere’s everything we currently have for your session. Please review it so the program and production teams are working from the right details.\n\n" + strings.Join(talks, "\n\n---\n\n") + "\n\n[Add or update your slides and GitHub repository in the speaker dashboard →](" + conferenceSpeakerDashboardLink(data.DashboardLink, conf.Tag) + ")"
			}
		}
	}
	return data
}

func conferenceSpeakerTalkMarkdown(conf *types.Conf, proposal *types.Proposal, confTalk *types.ConfTalk, recipientSpeakerConfID string, speakers []getters.ProposalSpeakerSummary) string {
	if proposal == nil {
		return ""
	}
	blocks := []string{"#### " + strings.TrimSpace(proposal.Title)}
	if description := strings.TrimSpace(proposal.Description); description != "" {
		blocks = append(blocks, description)
	}
	var details []string
	if talkType := conferenceTalkTypeLabel(proposal.TalkType); talkType != "" {
		details = append(details, "- **Type:** "+talkType)
	}
	if confTalk != nil && confTalk.Sched != nil && !confTalk.Sched.Start.IsZero() {
		details = append(details, "- **When:** "+conferenceTalkTimeLabel(conf, confTalk.Sched))
	} else {
		details = append(details, "- **When:** Not scheduled yet")
	}
	if confTalk != nil {
		if venue := strings.TrimSpace(types.NameVenue(confTalk.Venue)); venue != "" && venue != "Not Listed Yet" {
			details = append(details, "- **Stage:** "+venue)
		}
		if section := strings.TrimSpace(confTalk.Section); section != "" {
			details = append(details, "- **Track:** "+section)
		}
	}
	var coSpeakers []string
	for _, speaker := range speakers {
		if speaker.SpeakerConfID == recipientSpeakerConfID {
			continue
		}
		if name := strings.TrimSpace(speaker.Name); name != "" {
			coSpeakers = append(coSpeakers, name)
		}
	}
	if len(coSpeakers) > 0 {
		details = append(details, "- **Other speakers:** "+humanList(coSpeakers))
	}
	if confTalk != nil && strings.TrimSpace(confTalk.SlidesURL) != "" {
		details = append(details, "- **Slides:** [View slides]("+strings.TrimSpace(confTalk.SlidesURL)+")")
	} else {
		details = append(details, "- **Slides:** Not added yet")
	}
	if confTalk != nil && strings.TrimSpace(confTalk.GithubRepoURL) != "" {
		details = append(details, "- **GitHub repository:** [View repository]("+strings.TrimSpace(confTalk.GithubRepoURL)+")")
	} else {
		details = append(details, "- **GitHub repository:** Not added yet")
	}
	blocks = append(blocks, strings.Join(details, "\n"))
	return strings.Join(blocks, "\n\n")
}

func conferenceTalkTypeLabel(talkType string) string {
	talkType = strings.TrimSpace(strings.ReplaceAll(talkType, "-", " "))
	if talkType == "" {
		return ""
	}
	words := strings.Fields(talkType)
	for i := range words {
		words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
	}
	return strings.Join(words, " ")
}

func conferenceTalkTimeLabel(conf *types.Conf, scheduled *types.Times) string {
	if scheduled == nil || scheduled.Start.IsZero() {
		return "Not scheduled yet"
	}
	loc := time.UTC
	if conf != nil {
		loc = conf.Loc()
	}
	start := scheduled.Start.In(loc)
	label := start.Format("Monday, January 2 at 3:04 PM MST")
	if scheduled.End != nil && !scheduled.End.IsZero() {
		label += "–" + scheduled.End.In(loc).Format("3:04 PM MST")
	}
	return label
}

func conferenceSpeakerDashboardLink(dashboardLink, confTag string) string {
	dashboardLink = strings.TrimSpace(dashboardLink)
	if dashboardLink == "" {
		return ""
	}
	return dashboardLink + "#talks-" + url.PathEscape(strings.TrimSpace(confTag))
}

func ConferenceMissivesTestAutomation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	if ctx.InProduction {
		http.NotFound(w, r)
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if !ctx.Env.MailOff && strings.TrimSpace(ctx.Env.DevEmailOverride) == "" {
		redirectConferenceMissives(w, r, conf.Tag, "", "Set DEV_EMAIL_OVERRIDE before testing event-email delivery")
		return
	}
	simulatedAt := time.Now()
	if raw := strings.TrimSpace(r.FormValue("simulated_at")); raw != "" {
		parsed, parseErr := time.ParseInLocation("2006-01-02T15:04", raw, conf.Loc())
		if parseErr != nil {
			redirectConferenceMissives(w, r, conf.Tag, "", "Invalid simulated date and time")
			return
		}
		simulatedAt = parsed
	}
	runConferenceEmailDraftAutomationForConference(ctx, conf, simulatedAt)
	redirectConferenceMissives(w, r, conf.Tag, "Event email automation checked at "+simulatedAt.In(conf.Loc()).Format("Mon, Jan 2 at 3:04 PM MST"), "")
}
