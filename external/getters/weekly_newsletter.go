package getters

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

type WeeklyNewsletterTalk struct {
	ConfTag      string
	ConfTitle    string
	TalkID       string
	Title        string
	Description  string
	SpeakerNames string
	YouTubeURL   string
	PublishAt    time.Time
}

type WeeklyNewsletterSpeakerGroup struct {
	ConfTag   string
	ConfTitle string
	DateDesc  string
	Location  string
	Speakers  []WeeklyNewsletterSpeaker
}

type WeeklyNewsletterSpeaker struct {
	Name       string
	Company    string
	XURL       string
	NostrURL   string
	WebsiteURL string
	ProfileURL string // Legacy preferred profile; retained for older callers.
}

type WeeklyNewsletterTicketChange struct {
	Conf    *types.Conf
	Current *types.ConfTicket
	Next    *types.ConfTicket
}

type WeeklyNewsletterHackathonWinner struct {
	ConfTag       string
	CompetitionID string
	Competition   string
	ProjectID     string
	ProjectTitle  string
	ProjectNumber *int
	Awards        string
	FinalizedAt   time.Time
}

type WeeklyNewsletterSponsor struct {
	Name  string
	URL   string
	Level string
}

type WeeklyNewsletterSponsorGroup struct {
	ConfTag   string
	ConfTitle string
	Sponsors  []WeeklyNewsletterSponsor
}

type WeeklyNewsletterMerchUpdate struct {
	Slug       string
	Name       string
	Kind       string
	OccurredAt time.Time
}

type WeeklyNewsletterUpdateBundle struct {
	Talks              []WeeklyNewsletterTalk
	Broadcasts         []WeeklyNewsletterTalk
	TalkOfWeek         *WeeklyNewsletterTalk
	SpeakerGroups      []WeeklyNewsletterSpeakerGroup
	TicketChanges      []WeeklyNewsletterTicketChange
	HackathonWinners   []WeeklyNewsletterHackathonWinner
	NewSponsorGroups   []WeeklyNewsletterSponsorGroup
	SupportingSponsors []WeeklyNewsletterSponsor
	MerchUpdates       []WeeklyNewsletterMerchUpdate
}

// WeeklyNewsletterUpdates returns the database-backed updates relevant to an
// issue scheduled for issueSendAt. Talks and other lookback items use the
// previous seven-day window; talks are capped at the time the draft is built so
// the newsletter never links readers to a recording that is not public yet.
// Ticket changes use a fourteen-day horizon.
func WeeklyNewsletterUpdates(ctx *config.AppContext, issueSendAt time.Time) (*WeeklyNewsletterUpdateBundle, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	updates := &WeeklyNewsletterUpdateBundle{}
	var err error
	talkStart, talkEnd := weeklyNewsletterTalkWindow(issueSendAt, time.Now())
	updates.TalkOfWeek, err = weeklyNewsletterTalkOfWeek(ctx, talkEnd)
	if err != nil {
		return nil, err
	}
	publishedTalks, err := weeklyNewsletterTalks(ctx, talkStart, talkEnd)
	if err != nil {
		return nil, err
	}
	updates.Talks, updates.Broadcasts = organizeWeeklyNewsletterTalks(publishedTalks, updates.TalkOfWeek)
	updates.SpeakerGroups, err = weeklyNewsletterSpeakers(ctx, issueSendAt.AddDate(0, 0, -7), issueSendAt)
	if err != nil {
		return nil, err
	}
	updates.HackathonWinners, err = weeklyNewsletterHackathonWinners(ctx, issueSendAt.AddDate(0, 0, -7), issueSendAt)
	if err != nil {
		return nil, err
	}
	updates.TicketChanges, err = weeklyNewsletterTicketChanges(ctx, issueSendAt, issueSendAt.AddDate(0, 0, 14))
	if err != nil {
		return nil, err
	}
	updates.NewSponsorGroups, err = weeklyNewsletterNewSponsors(ctx, issueSendAt.AddDate(0, 0, -7), issueSendAt)
	if err != nil {
		return nil, err
	}
	updates.MerchUpdates, err = weeklyNewsletterMerchUpdates(ctx, issueSendAt.AddDate(0, 0, -7), issueSendAt)
	if err != nil {
		return nil, err
	}
	updates.SupportingSponsors, err = weeklyNewsletterSupportingSponsors(ctx, issueSendAt)
	if err != nil {
		return nil, err
	}
	return updates, nil
}

func weeklyNewsletterMerchUpdates(ctx *config.AppContext, start, end time.Time) ([]WeeklyNewsletterMerchUpdate, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH added AS (
			SELECT p.slug, p.name, 'added'::text AS kind, p.published_at AS occurred_at
			FROM merch_products p
			WHERE p.status = 'published'
				AND p.published_at >= $1 AND p.published_at < $2
				AND (p.available_from IS NULL OR p.available_from < $2)
				AND (p.available_until IS NULL OR p.available_until >= $1)
				AND EXISTS (
					SELECT 1
					FROM merch_variants available_variant
					WHERE available_variant.product_id = p.id
						AND available_variant.status = 'active'
						AND (
							available_variant.inventory_policy IN ('allow_backorder', 'unlimited')
							OR coalesce((
								SELECT sum(stock_event.quantity_delta)
								FROM merch_inventory_events stock_event
								WHERE stock_event.variant_id = available_variant.id
							), 0) > 0
						)
				)
		), restocked AS (
			SELECT p.slug, p.name, 'restocked'::text AS kind, max(event.occurred_at) AS occurred_at
			FROM merch_inventory_events event
			JOIN merch_variants v ON v.id = event.variant_id
			JOIN merch_products p ON p.id = v.product_id
			WHERE event.occurred_at >= $1 AND event.occurred_at < $2
				AND event.quantity_delta > 0
				AND event.event_type IN ('increase', 'adjustment')
				AND p.status = 'published'
				AND (p.published_at IS NULL OR p.published_at < $1)
				AND (p.available_from IS NULL OR p.available_from < $2)
				AND (p.available_until IS NULL OR p.available_until >= $1)
				AND EXISTS (
					SELECT 1
					FROM merch_variants available_variant
					WHERE available_variant.product_id = p.id
						AND available_variant.status = 'active'
						AND (
							available_variant.inventory_policy IN ('allow_backorder', 'unlimited')
							OR coalesce((
								SELECT sum(stock_event.quantity_delta)
								FROM merch_inventory_events stock_event
								WHERE stock_event.variant_id = available_variant.id
							), 0) > 0
						)
				)
			GROUP BY p.id, p.slug, p.name
		)
		SELECT slug, name, kind, occurred_at FROM added
		UNION ALL
		SELECT slug, name, kind, occurred_at FROM restocked
		ORDER BY occurred_at, name
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query weekly newsletter merch updates: %w", err)
	}
	defer rows.Close()
	var out []WeeklyNewsletterMerchUpdate
	for rows.Next() {
		var update WeeklyNewsletterMerchUpdate
		if err := rows.Scan(&update.Slug, &update.Name, &update.Kind, &update.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan weekly newsletter merch update: %w", err)
		}
		out = append(out, update)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly newsletter merch updates: %w", err)
	}
	return out, nil
}

type weeklyNewsletterTalkCandidate struct {
	Talk               WeeklyNewsletterTalk
	ConfID             string
	TalkType           string
	Venue              string
	ConfTimezone       string
	ScheduledStart     *time.Time
	ConfEnd            *time.Time
	SpeakerIDs         []string
	Recent             bool
	SamePreviousConf   bool
	RepeatsPrevSpeaker bool
}

func weeklyNewsletterTalkOfWeek(ctx *config.AppContext, availableAt time.Time) (*WeeklyNewsletterTalk, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH previous AS (
			SELECT ct.conference_id,
				coalesce(array_agg(DISTINCT sc.speaker_id::text)
					FILTER (WHERE sc.speaker_id IS NOT NULL), '{}') AS speaker_ids
			FROM weekly_newsletter_featured_talks f
			JOIN missives m ON m.id = f.missive_id
			JOIN conf_talks ct ON ct.id = f.conf_talk_id
			LEFT JOIN proposals_speaker_confs psc ON psc.proposal_id = ct.proposal_id
			LEFT JOIN speaker_confs sc ON sc.id = psc.speaker_conf_id
			GROUP BY f.missive_id, m.created_at, ct.conference_id
			ORDER BY m.created_at DESC
			LIMIT 1
		)
		SELECT c.id::text, c.tag, c.description, ct.id::text,
			coalesce(nullif(r.talk_name, ''), p.title, 'Untitled talk'),
			coalesce(p.description, ''),
			coalesce(string_agg(DISTINCT people.name, ', ' ORDER BY people.name)
				FILTER (WHERE people.name IS NOT NULL), ''),
			r.youtube_url, r.publish_at, coalesce(p.talk_type, ''), ct.venue, c.timezone,
			ct.scheduled_start, c.end_date,
			coalesce(array_agg(DISTINCT sc.speaker_id::text)
				FILTER (WHERE sc.speaker_id IS NOT NULL), '{}'),
			r.publish_at >= $1::timestamptz - interval '7 days',
			coalesce(c.id = previous.conference_id, false),
			coalesce((array_agg(DISTINCT sc.speaker_id::text)
				FILTER (WHERE sc.speaker_id IS NOT NULL)) && previous.speaker_ids, false)
		FROM recordings r
		JOIN conf_talks ct ON ct.id = r.conf_talk_id
		JOIN conferences c ON c.id = ct.conference_id
		LEFT JOIN proposals p ON p.id = ct.proposal_id
		LEFT JOIN proposals_speaker_confs psc ON psc.proposal_id = p.id
		LEFT JOIN speaker_confs sc ON sc.id = psc.speaker_conf_id
		LEFT JOIN people ON people.id = sc.speaker_id
		LEFT JOIN previous ON true
		WHERE r.publish_at >= $1::timestamptz - interval '90 days'
			AND r.publish_at <= $1::timestamptz
			AND nullif(trim(r.youtube_url), '') IS NOT NULL
			AND ct.archived_at IS NULL
			AND c.publication_status = 'published'
			AND (p.id IS NULL OR p.status IN ('Accepted', 'Scheduled'))
			AND NOT EXISTS (
				SELECT 1 FROM weekly_newsletter_featured_talks used
				WHERE used.conf_talk_id = ct.id
			)
		GROUP BY c.id, c.tag, c.description, ct.id, r.talk_name, p.title,
			p.description, r.youtube_url, r.publish_at, p.talk_type, ct.venue, c.timezone,
			ct.scheduled_start, c.end_date, previous.conference_id, previous.speaker_ids
	`, availableAt)
	if err != nil {
		return nil, fmt.Errorf("query weekly newsletter talk of the week: %w", err)
	}
	defer rows.Close()

	var candidates []weeklyNewsletterTalkCandidate
	for rows.Next() {
		var candidate weeklyNewsletterTalkCandidate
		if err := rows.Scan(
			&candidate.ConfID, &candidate.Talk.ConfTag, &candidate.Talk.ConfTitle,
			&candidate.Talk.TalkID, &candidate.Talk.Title, &candidate.Talk.Description,
			&candidate.Talk.SpeakerNames, &candidate.Talk.YouTubeURL, &candidate.Talk.PublishAt,
			&candidate.TalkType, &candidate.Venue, &candidate.ConfTimezone, &candidate.ScheduledStart, &candidate.ConfEnd,
			&candidate.SpeakerIDs, &candidate.Recent, &candidate.SamePreviousConf,
			&candidate.RepeatsPrevSpeaker,
		); err != nil {
			return nil, fmt.Errorf("scan weekly newsletter talk of the week: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly newsletter talk of the week: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return weeklyNewsletterTalkCandidateLess(candidates[i], candidates[j])
	})
	return &candidates[0].Talk, nil
}

func weeklyNewsletterTalkCandidateLess(a, b weeklyNewsletterTalkCandidate) bool {
	if a.Recent != b.Recent {
		return a.Recent
	}
	aPanel, bPanel := strings.EqualFold(strings.TrimSpace(a.TalkType), "panel"), strings.EqualFold(strings.TrimSpace(b.TalkType), "panel")
	if aPanel != bPanel {
		return aPanel
	}
	aMain, bMain := weeklyNewsletterMainStage(a.Venue), weeklyNewsletterMainStage(b.Venue)
	if aMain != bMain {
		return aMain
	}
	aFinal, bFinal := weeklyNewsletterFinalDay(a), weeklyNewsletterFinalDay(b)
	if aFinal != bFinal {
		return aFinal
	}
	if a.SamePreviousConf != b.SamePreviousConf {
		return !a.SamePreviousConf
	}
	if a.RepeatsPrevSpeaker != b.RepeatsPrevSpeaker {
		return !a.RepeatsPrevSpeaker
	}
	if a.ScheduledStart != nil && b.ScheduledStart != nil && !a.ScheduledStart.Equal(*b.ScheduledStart) {
		return a.ScheduledStart.After(*b.ScheduledStart)
	}
	if (a.ScheduledStart != nil) != (b.ScheduledStart != nil) {
		return a.ScheduledStart != nil
	}
	if !a.Talk.PublishAt.Equal(b.Talk.PublishAt) {
		return a.Talk.PublishAt.After(b.Talk.PublishAt)
	}
	return a.Talk.TalkID < b.Talk.TalkID
}

func weeklyNewsletterMainStage(venue string) bool {
	switch strings.ToLower(strings.TrimSpace(venue)) {
	case "one", "main", "main stage", "p2pkh":
		return true
	default:
		return false
	}
}

func weeklyNewsletterFinalDay(candidate weeklyNewsletterTalkCandidate) bool {
	if candidate.ScheduledStart == nil || candidate.ConfEnd == nil {
		return false
	}
	loc := time.UTC
	if configured, err := time.LoadLocation(strings.TrimSpace(candidate.ConfTimezone)); err == nil {
		loc = configured
	}
	y, m, d := candidate.ScheduledStart.In(loc).Date()
	ey, em, ed := candidate.ConfEnd.In(loc).Date()
	return y == ey && m == em && d == ed
}

func omitWeeklyNewsletterTalk(talks []WeeklyNewsletterTalk, talkID string) []WeeklyNewsletterTalk {
	filtered := talks[:0]
	for _, talk := range talks {
		if talk.TalkID != talkID {
			filtered = append(filtered, talk)
		}
	}
	return filtered
}

func organizeWeeklyNewsletterTalks(published []WeeklyNewsletterTalk, featured *WeeklyNewsletterTalk) ([]WeeklyNewsletterTalk, []WeeklyNewsletterTalk) {
	if len(published) > 3 {
		return nil, published[len(published)-3:]
	}
	if featured == nil {
		return published, nil
	}
	return omitWeeklyNewsletterTalk(published, featured.TalkID), nil
}

func weeklyNewsletterTalkWindow(issueSendAt, builtAt time.Time) (time.Time, time.Time) {
	end := builtAt
	if issueSendAt.Before(end) {
		end = issueSendAt
	}
	return end.AddDate(0, 0, -7), end
}

func weeklyNewsletterTalks(ctx *config.AppContext, start, end time.Time) ([]WeeklyNewsletterTalk, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT c.tag, c.description, ct.id::text,
			coalesce(nullif(r.talk_name, ''), p.title, 'Untitled talk'),
			coalesce(string_agg(DISTINCT people.name, ', ' ORDER BY people.name)
				FILTER (WHERE people.name IS NOT NULL), ''),
			r.youtube_url, r.publish_at
		FROM recordings r
		JOIN conf_talks ct ON ct.id = r.conf_talk_id
		JOIN conferences c ON c.id = ct.conference_id
		LEFT JOIN proposals p ON p.id = ct.proposal_id
		LEFT JOIN proposals_speaker_confs psc ON psc.proposal_id = p.id
		LEFT JOIN speaker_confs sc ON sc.id = psc.speaker_conf_id
		LEFT JOIN people ON people.id = sc.speaker_id
		WHERE r.publish_at >= $1 AND r.publish_at < $2
			AND ct.archived_at IS NULL
			AND c.publication_status = 'published'
			AND (p.id IS NULL OR p.status IN ('Accepted', 'Scheduled'))
		GROUP BY c.tag, c.description, ct.id, r.talk_name, p.title, r.youtube_url, r.publish_at
		ORDER BY r.publish_at, c.tag, coalesce(nullif(r.talk_name, ''), p.title, 'Untitled talk')
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query weekly newsletter talks: %w", err)
	}
	defer rows.Close()
	var out []WeeklyNewsletterTalk
	for rows.Next() {
		var item WeeklyNewsletterTalk
		if err := rows.Scan(&item.ConfTag, &item.ConfTitle, &item.TalkID, &item.Title, &item.SpeakerNames, &item.YouTubeURL, &item.PublishAt); err != nil {
			return nil, fmt.Errorf("scan weekly newsletter talk: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly newsletter talks: %w", err)
	}
	return out, nil
}

func weeklyNewsletterSpeakers(ctx *config.AppContext, start, end time.Time) ([]WeeklyNewsletterSpeakerGroup, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT c.tag, c.description, c.date_desc, c.location, people.name,
			coalesce(nullif(sc.company, ''), nullif(people.company, ''), ''),
			people.twitter_handle, people.nostr, people.website_url
		FROM speaker_confs sc
		JOIN people ON people.id = sc.speaker_id
		JOIN proposals_speaker_confs psc ON psc.speaker_conf_id = sc.id
		JOIN proposals p ON p.id = psc.proposal_id
		JOIN conferences c ON c.id = p.conference_id
		WHERE sc.accepted_at >= $1 AND sc.accepted_at < $2
			AND p.status IN ('Accepted', 'Scheduled')
			AND c.publication_status = 'published'
			AND (c.end_date IS NULL OR c.end_date >= $2)
		GROUP BY c.start_date, c.tag, c.description, c.date_desc, c.location,
			people.id, people.name, sc.company, people.company,
			people.twitter_handle, people.nostr, people.website_url
		ORDER BY c.start_date, c.tag, people.name
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query weekly newsletter speakers: %w", err)
	}
	defer rows.Close()
	var out []WeeklyNewsletterSpeakerGroup
	byTag := map[string]int{}
	for rows.Next() {
		var tag, title, dateDesc, location, name, company, twitter, nostr, website string
		if err := rows.Scan(&tag, &title, &dateDesc, &location, &name, &company, &twitter, &nostr, &website); err != nil {
			return nil, fmt.Errorf("scan weekly newsletter speaker: %w", err)
		}
		idx, ok := byTag[tag]
		if !ok {
			idx = len(out)
			byTag[tag] = idx
			out = append(out, WeeklyNewsletterSpeakerGroup{ConfTag: tag, ConfTitle: title, DateDesc: dateDesc, Location: location})
		}
		out[idx].Speakers = append(out[idx].Speakers, WeeklyNewsletterSpeaker{
			Name:       name,
			Company:    company,
			XURL:       weeklyNewsletterSpeakerXURL(twitter),
			NostrURL:   weeklyNewsletterSpeakerNostrURL(nostr),
			WebsiteURL: strings.TrimSpace(website),
			ProfileURL: weeklyNewsletterSpeakerProfileURL(twitter, nostr, website),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly newsletter speakers: %w", err)
	}
	return out, nil
}

func weeklyNewsletterSpeakerXURL(twitter string) string {
	return types.ParseTwitter(twitter).Link()
}

func weeklyNewsletterSpeakerNostrURL(nostr string) string {
	if nostr = strings.TrimSpace(nostr); nostr != "" {
		return "https://njump.me/" + nostr
	}
	return ""
}

func weeklyNewsletterSpeakerProfileURL(twitter, nostr, website string) string {
	if profile := weeklyNewsletterSpeakerXURL(twitter); profile != "" {
		return profile
	}
	if profile := weeklyNewsletterSpeakerNostrURL(nostr); profile != "" {
		return profile
	}
	return strings.TrimSpace(website)
}

func weeklyNewsletterTicketChanges(ctx *config.AppContext, start, end time.Time) ([]WeeklyNewsletterTicketChange, error) {
	confs, err := ListConfs(ctx)
	if err != nil {
		return nil, fmt.Errorf("load conferences for weekly ticket changes: %w", err)
	}
	var out []WeeklyNewsletterTicketChange
	for _, conf := range confs {
		if conf == nil || !conf.IsPublished() || (!conf.EndDate.IsZero() && conf.EndDate.Before(start)) {
			continue
		}
		sold, err := SoldTix(ctx, conf)
		if err != nil {
			return nil, fmt.Errorf("load sold tickets for %s: %w", conf.Tag, err)
		}
		current := types.CurrentConfTicketAt(conf.Tickets, sold, start)
		if current == nil || !current.SalesEndAt.Before(end) {
			continue
		}
		next := types.NextConfTicketAfter(conf.Tickets, current, sold)
		if next == nil || next.StandardPrice() <= current.StandardPrice() {
			continue
		}
		out = append(out, WeeklyNewsletterTicketChange{Conf: conf, Current: current, Next: next})
	}
	return out, nil
}

func weeklyNewsletterHackathonWinners(ctx *config.AppContext, start, end time.Time) ([]WeeklyNewsletterHackathonWinner, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT c.tag, competitions.id::text, competitions.title,
			projects.id::text, projects.title, projects.project_number,
			string_agg(DISTINCT awards.title, ', ' ORDER BY awards.title),
			competitions.results_finalized_at
		FROM competitions
		JOIN conferences c ON c.id = competitions.conference_id
		JOIN awards ON awards.competition_id = competitions.id AND awards.archived_at IS NULL
		JOIN project_awards ON project_awards.award_id = awards.id
		JOIN projects ON projects.id = project_awards.project_id
		WHERE competitions.results_finalized_at >= $1
			AND competitions.results_finalized_at < $2
			AND competitions.visibility = 'public'
			AND competitions.public_gallery_enabled
			AND (competitions.public_gallery_at IS NULL OR competitions.public_gallery_at <= $2)
			AND c.publication_status = 'published'
			AND projects.status IN ('submitted', 'advanced')
		GROUP BY c.tag, competitions.id, competitions.title, projects.id,
			projects.title, projects.project_number, competitions.results_finalized_at
		ORDER BY coalesce(min(awards.award_rank), 2147483647),
			count(DISTINCT awards.id) DESC, competitions.results_finalized_at DESC,
			competitions.title, projects.project_number NULLS LAST, projects.title
		LIMIT 3
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query weekly newsletter hackathon winners: %w", err)
	}
	defer rows.Close()
	var out []WeeklyNewsletterHackathonWinner
	for rows.Next() {
		var item WeeklyNewsletterHackathonWinner
		var projectNumber sql.NullInt64
		if err := rows.Scan(&item.ConfTag, &item.CompetitionID, &item.Competition, &item.ProjectID, &item.ProjectTitle, &projectNumber, &item.Awards, &item.FinalizedAt); err != nil {
			return nil, fmt.Errorf("scan weekly newsletter hackathon winner: %w", err)
		}
		if projectNumber.Valid {
			n := int(projectNumber.Int64)
			item.ProjectNumber = &n
		}
		item.Awards = strings.TrimSpace(item.Awards)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly newsletter hackathon winners: %w", err)
	}
	return out, nil
}

func weeklyNewsletterNewSponsors(ctx *config.AppContext, start, end time.Time) ([]WeeklyNewsletterSponsorGroup, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT c.tag, c.description,
			coalesce(nullif(trim(o.name), ''), s.name), o.website_url, s.level
		FROM sponsorships s
		JOIN sponsorships_conferences sc ON sc.sponsorship_id = s.id
		JOIN conferences c ON c.id = sc.conference_id
		LEFT JOIN organizations o ON o.id = s.organization_id
		WHERE greatest(s.created_at, s.updated_at) >= $1
			AND greatest(s.created_at, s.updated_at) < $2
			AND lower(trim(s.status)) IN ('paid', 'committed')
			AND s.archived_at IS NULL
			AND c.publication_status = 'published'
			AND (c.end_date IS NULL OR c.end_date >= $2)
		ORDER BY c.start_date, c.tag, lower(coalesce(nullif(trim(o.name), ''), s.name))
	`, start, end)
	if err != nil {
		return nil, fmt.Errorf("query weekly newsletter new sponsors: %w", err)
	}
	defer rows.Close()

	var out []WeeklyNewsletterSponsorGroup
	byTag := make(map[string]int)
	for rows.Next() {
		var tag, title string
		var sponsor WeeklyNewsletterSponsor
		if err := rows.Scan(&tag, &title, &sponsor.Name, &sponsor.URL, &sponsor.Level); err != nil {
			return nil, fmt.Errorf("scan weekly newsletter new sponsor: %w", err)
		}
		idx, ok := byTag[tag]
		if !ok {
			idx = len(out)
			byTag[tag] = idx
			out = append(out, WeeklyNewsletterSponsorGroup{ConfTag: tag, ConfTitle: title})
		}
		out[idx].Sponsors = append(out[idx].Sponsors, sponsor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly newsletter new sponsors: %w", err)
	}
	return out, nil
}

func weeklyNewsletterSupportingSponsors(ctx *config.AppContext, issueSendAt time.Time) ([]WeeklyNewsletterSponsor, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT coalesce(nullif(trim(o.name), ''), min(s.name)),
			coalesce(nullif(trim(o.website_url), ''), ''),
			min(s.level)
		FROM sponsorships s
		JOIN sponsorships_conferences sc ON sc.sponsorship_id = s.id
		JOIN conferences c ON c.id = sc.conference_id
		LEFT JOIN organizations o ON o.id = s.organization_id
		WHERE lower(trim(s.status)) IN ('paid', 'committed')
			AND s.archived_at IS NULL
			AND c.publication_status = 'published'
			AND (c.end_date IS NULL OR c.end_date >= $1)
			AND lower(trim(regexp_replace(s.level, '[[:space:]]+(sponsors?|level)[[:space:]]*$', '', 'i'))) IN ('headline', 'gold')
		GROUP BY coalesce(o.id, s.id), o.name, o.website_url
		ORDER BY lower(coalesce(nullif(trim(o.name), ''), min(s.name)))
	`, issueSendAt)
	if err != nil {
		return nil, fmt.Errorf("query weekly newsletter supporting sponsors: %w", err)
	}
	defer rows.Close()

	var out []WeeklyNewsletterSponsor
	for rows.Next() {
		var sponsor WeeklyNewsletterSponsor
		if err := rows.Scan(&sponsor.Name, &sponsor.URL, &sponsor.Level); err != nil {
			return nil, fmt.Errorf("scan weekly newsletter supporting sponsor: %w", err)
		}
		out = append(out, sponsor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly newsletter supporting sponsors: %w", err)
	}
	return out, nil
}
