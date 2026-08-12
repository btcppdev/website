package getters

import (
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
	conferencemissives "btcpp-web/templates/missives"
	"github.com/jackc/pgx/v5/pgtype"
)

type conferenceCampaignDefault struct {
	Kind       string
	Audience   string
	SendHour   int
	DaysBefore int
}

func ClaimConferenceEmailBuilds(ctx *config.AppContext, now time.Time, limit int) ([]*types.ConferenceEmailOccurrence, error) {
	return claimConferenceEmailBuilds(ctx, now, limit, "")
}

func ClaimConferenceEmailBuildsForConference(ctx *config.AppContext, now time.Time, limit int, confID string) ([]*types.ConferenceEmailOccurrence, error) {
	return claimConferenceEmailBuilds(ctx, now, limit, strings.TrimSpace(confID))
}

func claimConferenceEmailBuilds(ctx *config.AppContext, now time.Time, limit int, confID string) ([]*types.ConferenceEmailOccurrence, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH due AS (
			SELECT o.id
			FROM conference_email_occurrences o
			JOIN conference_email_campaigns c ON c.id = o.campaign_id
			WHERE c.enabled
				AND (NULLIF($3, '') IS NULL OR c.conference_id = NULLIF($3, '')::uuid)
				AND o.build_at <= $1
				AND (
					o.status = 'planned'
					OR (o.status IN ('building', 'failed') AND o.missive_id IS NULL
						AND o.claimed_at < $1 - interval '20 minutes')
				)
			ORDER BY o.build_at, o.id
			FOR UPDATE OF o SKIP LOCKED
			LIMIT $2
		)
		UPDATE conference_email_occurrences o
		SET status = 'building', claimed_at = $1, last_error = ''
		FROM due
		WHERE o.id = due.id
		RETURNING o.id::text
	`, now, limit, confID)
	if err != nil {
		return nil, fmt.Errorf("claim conference email builds: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return loadConferenceEmailOccurrencesByIDs(ctx, ids)
}

func ClaimConferenceEmailSends(ctx *config.AppContext, now time.Time, limit int) ([]*types.ConferenceEmailOccurrence, error) {
	return claimConferenceEmailSends(ctx, now, limit, "")
}

func claimConferenceEmailSends(ctx *config.AppContext, now time.Time, limit int, confID string) ([]*types.ConferenceEmailOccurrence, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH due AS (
			SELECT o.id
			FROM conference_email_occurrences o
			JOIN conference_email_campaigns c ON c.id = o.campaign_id
			WHERE c.enabled AND o.send_at <= $1 AND o.missive_id IS NOT NULL
				AND (
					o.status = 'draft'
					OR (o.status IN ('sending', 'failed') AND o.claimed_at < $1 - interval '20 minutes')
				)
			ORDER BY o.send_at, o.id
			FOR UPDATE OF o SKIP LOCKED
			LIMIT $2
		)
		UPDATE conference_email_occurrences o
		SET status = 'sending', claimed_at = $1, last_error = ''
		FROM due
		WHERE o.id = due.id
		RETURNING o.id::text
	`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim conference email sends: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return loadConferenceEmailOccurrencesByIDs(ctx, ids)
}

func loadConferenceEmailOccurrencesByIDs(ctx *config.AppContext, ids []string) ([]*types.ConferenceEmailOccurrence, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT o.id::text, o.campaign_id::text, c.kind, c.title, c.audience,
			c.conference_id::text, conf.tag, o.occurrence_key, o.build_at, o.send_at,
			coalesce(o.missive_id::text, ''), coalesce(m.public_uid, 0),
			o.target_key, coalesce(o.target_email::text, ''), o.status, c.enabled,
			o.built_at, o.queued_at, o.sent_at, o.skipped_at, o.last_error
		FROM conference_email_occurrences o
		JOIN conference_email_campaigns c ON c.id = o.campaign_id
		JOIN conferences conf ON conf.id = c.conference_id
		LEFT JOIN missives m ON m.id = o.missive_id
		WHERE o.id::text = ANY($1::text[])
		ORDER BY o.send_at, o.id
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("load claimed conference email occurrences: %w", err)
	}
	defer rows.Close()
	var occurrences []*types.ConferenceEmailOccurrence
	for rows.Next() {
		var occurrence types.ConferenceEmailOccurrence
		var builtAt, queuedAt, sentAt, skippedAt pgtype.Timestamptz
		if err := rows.Scan(&occurrence.ID, &occurrence.CampaignID, &occurrence.CampaignKind,
			&occurrence.CampaignTitle, &occurrence.Audience, &occurrence.ConferenceID,
			&occurrence.ConferenceTag, &occurrence.OccurrenceKey, &occurrence.BuildAt,
			&occurrence.SendAt, &occurrence.MissiveID, &occurrence.MissiveUID,
			&occurrence.TargetKey, &occurrence.TargetEmail, &occurrence.Status,
			&occurrence.Enabled, &builtAt, &queuedAt, &sentAt, &skippedAt,
			&occurrence.LastError); err != nil {
			return nil, fmt.Errorf("scan claimed conference email occurrence: %w", err)
		}
		occurrence.BuiltAt = pgTimePtr(builtAt)
		occurrence.QueuedAt = pgTimePtr(queuedAt)
		occurrence.SentAt = pgTimePtr(sentAt)
		occurrence.SkippedAt = pgTimePtr(skippedAt)
		occurrences = append(occurrences, &occurrence)
	}
	return occurrences, rows.Err()
}

func GetConferenceEmailOccurrence(ctx *config.AppContext, confID, occurrenceID string) (*types.ConferenceEmailOccurrence, error) {
	occurrences, err := loadConferenceEmailOccurrencesByIDs(ctx, []string{occurrenceID})
	if err != nil {
		return nil, err
	}
	if len(occurrences) == 0 || occurrences[0].ConferenceID != confID {
		return nil, fmt.Errorf("conference email occurrence not found")
	}
	return occurrences[0], nil
}

func UpdateConferenceOccurrenceDraft(ctx *config.AppContext, confID, occurrenceID, title, markdown string) error {
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE missives m
		SET title = $3, markdown = $4
		FROM conference_email_occurrences o
		JOIN conference_email_campaigns c ON c.id = o.campaign_id
		WHERE o.id = $1::uuid AND c.conference_id = $2::uuid
			AND o.missive_id = m.id AND o.status = 'draft'
	`, occurrenceID, confID, strings.TrimSpace(title), markdown)
	if err != nil {
		return fmt.Errorf("update conference occurrence draft: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("editable conference occurrence draft not found")
	}
	return nil
}

func CreateConferenceOccurrenceDraft(ctx *config.AppContext, occurrence *types.ConferenceEmailOccurrence, title, markdown string, expiry *time.Time) (*mtypes.Letter, error) {
	if occurrence == nil {
		return nil, fmt.Errorf("conference email occurrence is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, fmt.Errorf("begin conference email draft: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	dedupeKey := "conference-email:" + occurrence.ID
	row := tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO missives
			(public_uid, title, markdown, send_at_expr, newsletters, only_for, expiry, dedupe_key, conference_id)
		VALUES ((SELECT COALESCE(max(public_uid), 0) + 1 FROM missives), $1, $2, $3, ARRAY[$4], $5, $6, $7, $8::uuid)
		ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL
		DO UPDATE SET dedupe_key = EXCLUDED.dedupe_key
		RETURNING id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
	`, strings.TrimSpace(title), markdown, occurrence.SendAt.Format(time.RFC3339),
		occurrence.ConferenceTag, mtypes.OnlyForTemplated, expiry, dedupeKey, occurrence.ConferenceID)
	letter, err := scanLetterPostgres(row)
	if err != nil {
		return nil, fmt.Errorf("insert conference email draft: %w", err)
	}
	result, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE conference_email_occurrences
		SET missive_id = $2::uuid, status = 'draft', built_at = now(), claimed_at = NULL, last_error = ''
		WHERE id = $1::uuid AND status = 'building'
	`, occurrence.ID, letter.PageID)
	if err != nil {
		return nil, fmt.Errorf("attach conference email draft: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, fmt.Errorf("conference email occurrence is no longer buildable")
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, fmt.Errorf("commit conference email draft: %w", err)
	}
	return letter, nil
}

func FailConferenceEmailOccurrence(ctx *config.AppContext, occurrenceID, status string, failure error) error {
	if status != "failed" && status != "planned" {
		status = "failed"
	}
	message := ""
	if failure != nil {
		message = failure.Error()
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE conference_email_occurrences
		SET status = $2, last_error = $3, claimed_at = now()
		WHERE id = $1::uuid
	`, occurrenceID, status, message)
	return err
}

func CompleteConferenceEmailOccurrence(ctx *config.AppContext, occurrenceID string, now time.Time) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE missives m
		SET sent_at = $2
		FROM conference_email_occurrences o
		WHERE o.id = $1::uuid AND o.missive_id = m.id
	`, occurrenceID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE conference_email_occurrences
		SET status = 'sent', queued_at = $2, sent_at = $2, claimed_at = NULL, last_error = ''
		WHERE id = $1::uuid
	`, occurrenceID, now); err != nil {
		return err
	}
	return tx.Commit(ctx.DatabaseContext())
}

func CancelConferenceEmailOccurrence(ctx *config.AppContext, confID, occurrenceID string) error {
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE conference_email_occurrences o
		SET status = 'cancelled', claimed_at = NULL
		FROM conference_email_campaigns c
		WHERE o.id = $1::uuid AND o.campaign_id = c.id AND c.conference_id = $2::uuid
			AND o.status IN ('planned', 'draft', 'sending', 'failed', 'paused')
	`, occurrenceID, confID)
	if err != nil {
		return fmt.Errorf("cancel conference email occurrence: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("conference email occurrence cannot be cancelled")
	}
	return nil
}

func ResetConferenceOccurrenceDraft(ctx *config.AppContext, confID, occurrenceID string) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var missiveID string
	err = tx.QueryRow(ctx.DatabaseContext(), `
		SELECT coalesce(o.missive_id::text, '')
		FROM conference_email_occurrences o
		JOIN conference_email_campaigns c ON c.id = o.campaign_id
		WHERE o.id = $1::uuid AND c.conference_id = $2::uuid AND o.status = 'draft'
		FOR UPDATE OF o
	`, occurrenceID, confID).Scan(&missiveID)
	if err != nil {
		return fmt.Errorf("lock conference email draft for rebuild: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE conference_email_occurrences
		SET missive_id = NULL, status = 'building', built_at = NULL, claimed_at = now(), last_error = ''
		WHERE id = $1::uuid
	`, occurrenceID); err != nil {
		return err
	}
	if missiveID != "" {
		if _, err := tx.Exec(ctx.DatabaseContext(), `DELETE FROM missives WHERE id = $1::uuid`, missiveID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx.DatabaseContext())
}

func BeginConferenceEmailDelivery(ctx *config.AppContext, occurrenceID, recipientKey, email, jobKey string) (*types.ConferenceEmailDelivery, bool, error) {
	var delivery types.ConferenceEmailDelivery
	var queuedAt pgtype.Timestamptz
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO conference_email_deliveries (occurrence_id, recipient_key, email, job_key)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (occurrence_id, recipient_key) DO UPDATE SET
			email = EXCLUDED.email,
			job_key = EXCLUDED.job_key
		RETURNING id::text, occurrence_id::text, recipient_key, email::text, job_key, status, queued_at, last_error
	`, occurrenceID, recipientKey, email, jobKey).Scan(&delivery.ID, &delivery.OccurrenceID,
		&delivery.RecipientKey, &delivery.Email, &delivery.JobKey, &delivery.Status,
		&queuedAt, &delivery.LastError)
	if err != nil {
		return nil, false, fmt.Errorf("prepare conference email delivery: %w", err)
	}
	delivery.QueuedAt = pgTimePtr(queuedAt)
	return &delivery, delivery.Status == "queued", nil
}

func FinishConferenceEmailDelivery(ctx *config.AppContext, deliveryID string, sendErr error) error {
	status := "queued"
	message := ""
	var queuedAt any = time.Now()
	if sendErr != nil {
		status = "failed"
		message = sendErr.Error()
		queuedAt = nil
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE conference_email_deliveries
		SET status = $2, queued_at = $3, last_error = $4
		WHERE id = $1::uuid
	`, deliveryID, status, queuedAt, message)
	return err
}

var conferenceCampaignDefaults = []conferenceCampaignDefault{
	{types.ConferenceCampaignAttendeeReminder70, "attendees", 10, 70},
	{types.ConferenceCampaignAttendeeReminder49, "attendees", 10, 49},
	{types.ConferenceCampaignAttendeeReminder28, "attendees", 10, 28},
	{types.ConferenceCampaignSpeakerReminder, "speakers", 10, 21},
	{types.ConferenceCampaignAttendeeFinal, "attendees", 10, 7},
	{types.ConferenceCampaignVolunteerOrient, "volunteers", 9, 0},
	{types.ConferenceCampaignSpeakerOnboarding, "speakers", 10, 0},
}

type ConferenceCampaignTiming struct {
	Kind     string
	BuildAt  time.Time
	SendAt   time.Time
	Audience string
}

// ConferenceCampaignTimings calculates the event-relative campaign clock in
// the venue's timezone. BuildAt is the same local wall-clock time one calendar
// day before SendAt so DST transitions do not shift editorial review time.
func ConferenceCampaignTimings(conf *types.Conf) []ConferenceCampaignTiming {
	if conf == nil || conf.StartDate.IsZero() {
		return nil
	}
	loc := conf.Loc()
	start := conf.StartDate.In(loc)
	out := make([]ConferenceCampaignTiming, 0, len(conferenceCampaignDefaults)-1)
	for _, def := range conferenceCampaignDefaults {
		if def.Kind == types.ConferenceCampaignSpeakerOnboarding {
			continue
		}
		var send time.Time
		if def.Kind == types.ConferenceCampaignVolunteerOrient {
			day := time.Date(start.Year(), start.Month(), start.Day(), def.SendHour, 0, 0, 0, loc)
			for {
				day = day.AddDate(0, 0, -1)
				if day.Weekday() == time.Thursday {
					send = day
					break
				}
			}
		} else {
			day := start.AddDate(0, 0, -def.DaysBefore)
			send = time.Date(day.Year(), day.Month(), day.Day(), def.SendHour, 0, 0, 0, loc)
		}
		out = append(out, ConferenceCampaignTiming{
			Kind: def.Kind, Audience: def.Audience, SendAt: send,
			BuildAt: send.AddDate(0, 0, -1),
		})
	}
	return out
}

func EnsureConferenceEmailCampaigns(ctx *config.AppContext, conf *types.Conf, now time.Time) error {
	if ctx == nil || ctx.DB == nil || conf == nil || conf.Ref == "" {
		return fmt.Errorf("conference campaign configuration is incomplete")
	}
	for _, campaignDefault := range conferenceCampaignDefaults {
		definition, err := conferencemissives.DefinitionForKind(campaignDefault.Kind)
		if err != nil {
			return fmt.Errorf("load %s event-email definition: %w", campaignDefault.Kind, err)
		}
		templateLetter, err := GetLetterFor(ctx, definition.OnlyFor)
		if err != nil {
			return fmt.Errorf("load %s event-email template: %w", campaignDefault.Kind, err)
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			INSERT INTO conference_email_campaigns
				(conference_id, kind, audience, title, markdown, send_time, template_missive_id)
			VALUES ($1::uuid, $2, $3, $4, $5, make_time($6, 0, 0), $7::uuid)
			ON CONFLICT (conference_id, kind) DO UPDATE SET
				template_missive_id = COALESCE(conference_email_campaigns.template_missive_id, EXCLUDED.template_missive_id)
		`, conf.Ref, campaignDefault.Kind, campaignDefault.Audience, types.ConferenceCampaignSubject(templateLetter.Title), templateLetter.Markdown,
			campaignDefault.SendHour, templateLetter.PageID); err != nil {
			return fmt.Errorf("ensure %s campaign for %s: %w", campaignDefault.Kind, conf.Tag, err)
		}
	}
	for _, timing := range ConferenceCampaignTimings(conf) {
		status := "planned"
		var skippedAt any
		if timing.SendAt.Before(now) {
			status = "skipped"
			skippedAt = now
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			INSERT INTO conference_email_occurrences
				(campaign_id, occurrence_key, build_at, send_at, status, skipped_at)
			SELECT id, $3, $4, $5, $6, $7
			FROM conference_email_campaigns
			WHERE conference_id = $1::uuid AND kind = $2
			ON CONFLICT (campaign_id, occurrence_key) DO UPDATE SET
				build_at = EXCLUDED.build_at,
				send_at = EXCLUDED.send_at,
				status = CASE
					WHEN conference_email_occurrences.status IN ('planned', 'skipped') THEN EXCLUDED.status
					ELSE conference_email_occurrences.status
				END,
				skipped_at = CASE
					WHEN conference_email_occurrences.status IN ('planned', 'skipped') THEN EXCLUDED.skipped_at
					ELSE conference_email_occurrences.skipped_at
				END
			WHERE conference_email_occurrences.status IN ('planned', 'skipped', 'paused', 'draft')
		`, conf.Ref, timing.Kind, timing.Kind, timing.BuildAt, timing.SendAt, status, skippedAt); err != nil {
			return fmt.Errorf("ensure %s occurrence for %s: %w", timing.Kind, conf.Tag, err)
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			UPDATE missives m
			SET send_at_expr = $3
			FROM conference_email_occurrences o
			JOIN conference_email_campaigns c ON c.id = o.campaign_id
			WHERE c.conference_id = $1::uuid AND c.kind = $2
				AND o.occurrence_key = $2 AND o.status = 'draft' AND o.missive_id = m.id
		`, conf.Ref, timing.Kind, timing.SendAt.Format(time.RFC3339)); err != nil {
			return fmt.Errorf("reschedule %s draft for %s: %w", timing.Kind, conf.Tag, err)
		}
	}
	return ensureSpeakerOnboardingOccurrences(ctx, conf, now)
}

func ensureSpeakerOnboardingOccurrences(ctx *config.AppContext, conf *types.Conf, now time.Time) error {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT sc.id::text, pe.email::text, sc.accepted_at
		FROM speaker_confs sc
		JOIN people p ON p.id = sc.speaker_id
		JOIN LATERAL (
			SELECT email FROM person_emails
			WHERE person_id = p.id
			ORDER BY is_primary DESC, created_at, id LIMIT 1
		) pe ON true
		JOIN speaker_confs_conferences scc ON scc.speaker_conf_id = sc.id
		WHERE scc.conference_id = $1::uuid
			AND sc.accepted_at IS NOT NULL
	`, conf.Ref)
	if err != nil {
		return fmt.Errorf("list accepted speakers for %s onboarding: %w", conf.Tag, err)
	}
	defer rows.Close()
	for rows.Next() {
		var speakerConfID, email string
		var acceptedAt time.Time
		if err := rows.Scan(&speakerConfID, &email, &acceptedAt); err != nil {
			return fmt.Errorf("scan accepted speaker for %s onboarding: %w", conf.Tag, err)
		}
		loc := conf.Loc()
		acceptedLocal := acceptedAt.In(loc)
		dueDay := acceptedLocal.AddDate(0, 0, 7)
		sendAt := time.Date(dueDay.Year(), dueDay.Month(), dueDay.Day(), 10, 0, 0, 0, loc)
		if conf.StartDate.Before(sendAt) {
			sendAt = acceptedAt
		}
		buildAt := sendAt.AddDate(0, 0, -1)
		if buildAt.Before(acceptedAt) {
			buildAt = acceptedAt
		}
		status := "planned"
		var skippedAt any
		if sendAt.Before(now) && now.Sub(sendAt) > 24*time.Hour {
			status = "skipped"
			skippedAt = now
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			INSERT INTO conference_email_occurrences
				(campaign_id, occurrence_key, build_at, send_at, target_key, target_email, status, skipped_at)
			SELECT id, $3, $4, $5, $6, $7, $8, $9
			FROM conference_email_campaigns
			WHERE conference_id = $1::uuid AND kind = $2
			ON CONFLICT (campaign_id, occurrence_key) DO UPDATE SET
				build_at = EXCLUDED.build_at,
				send_at = EXCLUDED.send_at,
				target_email = EXCLUDED.target_email,
				status = EXCLUDED.status,
				skipped_at = EXCLUDED.skipped_at
			WHERE conference_email_occurrences.status IN ('planned', 'skipped', 'paused')
		`, conf.Ref, types.ConferenceCampaignSpeakerOnboarding, "speaker-"+speakerConfID,
			buildAt, sendAt, speakerConfID, strings.TrimSpace(email), status, skippedAt); err != nil {
			return fmt.Errorf("ensure speaker onboarding %s/%s: %w", conf.Tag, speakerConfID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate accepted speakers for %s onboarding: %w", conf.Tag, err)
	}
	return nil
}

func ReconcileConferenceEmailCampaigns(ctx *config.AppContext, now time.Time) error {
	confs, err := ListConfs(ctx)
	if err != nil {
		return fmt.Errorf("list conferences for email campaigns: %w", err)
	}
	for _, conf := range confs {
		if conf == nil || conf.StartDate.IsZero() || (!conf.EndDate.IsZero() && conf.EndDate.Before(now)) {
			continue
		}
		if err := EnsureConferenceEmailCampaigns(ctx, conf, now); err != nil {
			return err
		}
	}
	return nil
}

func ListConferenceEmailCampaigns(ctx *config.AppContext, confID string) ([]*types.ConferenceEmailCampaign, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT c.id::text, c.conference_id::text, c.kind, c.audience, c.title, c.markdown,
			c.enabled, to_char(c.send_time, 'HH24:MI'), coalesce(c.template_missive_id::text, ''),
			coalesce(source.public_uid, 0), c.created_at, c.updated_at
		FROM conference_email_campaigns c
		LEFT JOIN missives source ON source.id = c.template_missive_id
		WHERE c.conference_id = $1::uuid
		ORDER BY c.send_time, c.kind
	`, confID)
	if err != nil {
		return nil, fmt.Errorf("list conference email campaigns: %w", err)
	}
	defer rows.Close()
	var campaigns []*types.ConferenceEmailCampaign
	for rows.Next() {
		var campaign types.ConferenceEmailCampaign
		if err := rows.Scan(&campaign.ID, &campaign.ConferenceID, &campaign.Kind,
			&campaign.Audience, &campaign.Title, &campaign.Markdown, &campaign.Enabled,
			&campaign.SendTime, &campaign.TemplateMissiveID, &campaign.TemplateMissiveUID,
			&campaign.CreatedAt, &campaign.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conference email campaign: %w", err)
		}
		campaigns = append(campaigns, &campaign)
	}
	return campaigns, rows.Err()
}

func UpdateConferenceEmailCampaign(ctx *config.AppContext, confID, campaignID, title, markdown string, enabled bool) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE conference_email_campaigns
		SET title = $3, markdown = $4, enabled = $5
		WHERE id = $1::uuid AND conference_id = $2::uuid
	`, campaignID, confID, strings.TrimSpace(title), markdown, enabled)
	if err != nil {
		return fmt.Errorf("update conference email campaign: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("conference email campaign not found")
	}
	return nil
}

func ListConferenceStandaloneMissives(ctx *config.AppContext, confID string) ([]*mtypes.Letter, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT m.id::text, m.public_uid, m.title, m.newsletters, m.only_for, m.markdown,
			m.send_at_expr, m.sent_at, m.expiry
		FROM missives m
		WHERE m.conference_id = $1::uuid
			AND NOT EXISTS (
				SELECT 1 FROM conference_email_occurrences o WHERE o.missive_id = m.id
			)
		ORDER BY m.public_uid DESC
	`, confID)
	if err != nil {
		return nil, fmt.Errorf("list standalone conference missives: %w", err)
	}
	defer rows.Close()
	var letters []*mtypes.Letter
	for rows.Next() {
		letter, err := scanLetterPostgres(rows)
		if err != nil {
			return nil, err
		}
		letters = append(letters, letter)
	}
	return letters, rows.Err()
}

func ListConferenceEmailOccurrences(ctx *config.AppContext, confID string) ([]*types.ConferenceEmailOccurrence, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT o.id::text, o.campaign_id::text, c.kind, c.title, c.audience,
			c.conference_id::text, o.occurrence_key, o.build_at, o.send_at,
			coalesce(o.missive_id::text, ''), coalesce(m.public_uid, 0),
			o.target_key, coalesce(o.target_email::text, ''), o.status, c.enabled,
			o.built_at, o.queued_at, o.sent_at, o.skipped_at, o.last_error
		FROM conference_email_occurrences o
		JOIN conference_email_campaigns c ON c.id = o.campaign_id
		LEFT JOIN missives m ON m.id = o.missive_id
		WHERE c.conference_id = $1::uuid
		ORDER BY o.send_at, c.kind, o.target_email
	`, confID)
	if err != nil {
		return nil, fmt.Errorf("list conference email occurrences: %w", err)
	}
	defer rows.Close()
	var occurrences []*types.ConferenceEmailOccurrence
	for rows.Next() {
		var occurrence types.ConferenceEmailOccurrence
		var builtAt, queuedAt, sentAt, skippedAt pgtype.Timestamptz
		if err := rows.Scan(&occurrence.ID, &occurrence.CampaignID, &occurrence.CampaignKind,
			&occurrence.CampaignTitle, &occurrence.Audience, &occurrence.ConferenceID,
			&occurrence.OccurrenceKey, &occurrence.BuildAt, &occurrence.SendAt,
			&occurrence.MissiveID, &occurrence.MissiveUID, &occurrence.TargetKey,
			&occurrence.TargetEmail, &occurrence.Status, &occurrence.Enabled,
			&builtAt, &queuedAt, &sentAt, &skippedAt, &occurrence.LastError); err != nil {
			return nil, fmt.Errorf("scan conference email occurrence: %w", err)
		}
		occurrence.BuiltAt = pgTimePtr(builtAt)
		occurrence.QueuedAt = pgTimePtr(queuedAt)
		occurrence.SentAt = pgTimePtr(sentAt)
		occurrence.SkippedAt = pgTimePtr(skippedAt)
		occurrences = append(occurrences, &occurrence)
	}
	return occurrences, rows.Err()
}
