package getters

import (
	"context"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"

	"github.com/jackc/pgx/v5"
)

// Publish slots belong to the shared channel, regardless of conference.
type RecordingPublishReservation struct {
	RecordingID     string
	Title           string
	ConferenceTag   string
	PublishAt       time.Time
	DurationSeconds int64
	Persistent      bool // an existing X broadcast remains reserved until updated
}

type RecordingScheduleUpdate struct {
	RecordingID     string
	PublishAt       *time.Time
	DurationSeconds int64 // populated from the database when saving
}

const RecordingBroadcastGap = 5 * time.Minute

type recordingScheduleQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func ListRecordingPublishReservations(ctx *config.AppContext) ([]RecordingPublishReservation, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	return listRecordingPublishReservations(ctx.DatabaseContext(), ctx.DB)
}

// RecordingScheduleReservations shares confirmed and pending reservations with
// the publishing calendar, under the same transaction lock as legacy workers.
func RecordingScheduleReservations(dbctx context.Context, db recordingScheduleQuerier) ([]RecordingPublishReservation, error) {
	return listRecordingPublishReservations(dbctx, db)
}

func listRecordingPublishReservations(dbctx context.Context, db recordingScheduleQuerier) ([]RecordingPublishReservation, error) {
	rows, err := db.Query(dbctx, `
		SELECT r.id::text, r.talk_name, coalesce(c.tag, ''), coalesce(r.publish_at, rb.started_at, rb.heartbeat_at), r.duration_seconds, false
		FROM recordings r
		LEFT JOIN conf_talks ct ON ct.id = r.conf_talk_id
		LEFT JOIN conferences c ON c.id = ct.conference_id
		LEFT JOIN recording_broadcasts rb ON rb.recording_id = r.id
		WHERE coalesce(r.publish_at, rb.started_at) + (r.duration_seconds + 300) * interval '1 second' >= now()
		   OR (rb.state = 'live' AND rb.heartbeat_at > now() - interval '5 minutes' AND r.duration_seconds = 0)
		UNION ALL
		SELECT coalesce(r.id::text, s.recording_id::text, ''),
			coalesce(nullif(r.talk_name, ''), 'YouTube post'), coalesce(c.tag, ''), s.scheduled_at, coalesce(r.duration_seconds, 0), false
		FROM social_posts s
		LEFT JOIN recordings r ON r.id = s.recording_id
			OR (s.recording_id IS NULL AND s.ref = 'recording:' || r.id::text || ':youtube')
		LEFT JOIN conf_talks ct ON ct.id = r.conf_talk_id
		LEFT JOIN conferences c ON c.id = ct.conference_id
		WHERE s.posted_to IN ('youtube', $1) AND s.scheduled_at + (coalesce(r.duration_seconds, 0) + 300) * interval '1 second' >= now()
		UNION ALL
		SELECT r.id::text, r.talk_name, coalesce(c.tag, ''), xb.scheduled_at, r.duration_seconds, true
		FROM recording_x_broadcasts xb
		JOIN recordings r ON r.id = xb.recording_id
		LEFT JOIN conf_talks ct ON ct.id = r.conf_talk_id
		LEFT JOIN conferences c ON c.id = ct.conference_id
		WHERE xb.scheduled_at + (r.duration_seconds + 300) * interval '1 second' >= now()
        UNION ALL
        SELECT r.id::text,r.talk_name,coalesce(c.tag,''),j.target_at,r.duration_seconds,true
        FROM recording_schedule_changes j JOIN recordings r ON r.id=j.recording_id
        JOIN conf_talks ct ON ct.id=r.conf_talk_id JOIN conferences c ON c.id=ct.conference_id
        WHERE j.state IN ('pending','syncing','failed','needs_review') AND j.target_at + (r.duration_seconds+300)*interval '1 second' >= now()
        UNION ALL
        SELECT r.id::text,r.talk_name,coalesce(c.tag,''),j.previous_at,r.duration_seconds,true
        FROM recording_schedule_changes j JOIN recordings r ON r.id=j.recording_id
        JOIN conf_talks ct ON ct.id=r.conf_talk_id JOIN conferences c ON c.id=ct.conference_id
        WHERE j.state IN ('pending','syncing','failed','needs_review') AND j.previous_at + (r.duration_seconds+300)*interval '1 second' >= now()

        ORDER BY 4, 1
	`, YouTubePublishChannel)
	if err != nil {
		return nil, fmt.Errorf("load shared recording schedule: %w", err)
	}
	defer rows.Close()
	var reservations []RecordingPublishReservation
	for rows.Next() {
		var reservation RecordingPublishReservation
		if err := rows.Scan(&reservation.RecordingID, &reservation.Title, &reservation.ConferenceTag, &reservation.PublishAt, &reservation.DurationSeconds, &reservation.Persistent); err != nil {
			return nil, fmt.Errorf("read shared recording schedule: %w", err)
		}
		reservations = append(reservations, reservation)
	}
	return reservations, rows.Err()
}

// SaveRecordingSchedule checks and reserves a whole batch before any external
// publishing calls. The transaction lock serializes saves from different event
// pages and application instances; a stale preview cannot double-book a slot.
func SaveRecordingSchedule(ctx *config.AppContext, updates []RecordingScheduleUpdate) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if len(updates) == 0 {
		return nil
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.BeginTx(dbctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin recording schedule: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if err := lockAndValidateRecordingSchedule(dbctx, tx, updates); err != nil {
		return err
	}
	for _, update := range updates {
		result, err := tx.Exec(dbctx, `UPDATE recordings SET publish_at = $2 WHERE id = $1::uuid`, update.RecordingID, update.PublishAt)
		if err != nil {
			return fmt.Errorf("save recording schedule: %w", err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("recording %s no longer exists; refresh the schedule", update.RecordingID)
		}
	}
	return tx.Commit(dbctx)
}

func lockAndValidateRecordingSchedule(dbctx context.Context, tx pgx.Tx, updates []RecordingScheduleUpdate) error {
	if _, err := tx.Exec(dbctx, `SELECT pg_advisory_xact_lock(7330192401)`); err != nil {
		return fmt.Errorf("lock shared recording schedule: %w", err)
	}
	reservations, err := listRecordingPublishReservations(dbctx, tx)
	if err != nil {
		return err
	}
	// Never trust a caller's duration at the save boundary.
	checked := append([]RecordingScheduleUpdate(nil), updates...)
	for i := range checked {
		if checked[i].PublishAt == nil || !checked[i].PublishAt.After(time.Now()) {
			continue
		}
		if err := tx.QueryRow(dbctx, `SELECT duration_seconds FROM recordings WHERE id = $1::uuid`, checked[i].RecordingID).Scan(&checked[i].DurationSeconds); err != nil {
			return fmt.Errorf("load recording runtime: %w", err)
		}
	}
	return validateRecordingSchedule(checked, reservations, time.Now())
}

func validateRecordingSchedule(updates []RecordingScheduleUpdate, reservations []RecordingPublishReservation, now time.Time) error {
	moving := make(map[string]bool, len(updates))
	for _, update := range updates {
		if strings.TrimSpace(update.RecordingID) == "" || moving[update.RecordingID] {
			return fmt.Errorf("recording schedule contains a missing or duplicate recording ID")
		}
		moving[update.RecordingID] = true
	}
	var occupied []RecordingPublishReservation
	for _, reservation := range reservations {
		if !moving[reservation.RecordingID] || reservation.Persistent {
			occupied = append(occupied, reservation)
		}
	}
	for _, update := range updates {
		// Historical publication metadata and clearing a schedule do not claim
		// a future channel slot.
		if update.PublishAt == nil || !update.PublishAt.After(now) {
			continue
		}
		if update.DurationSeconds <= 0 {
			return fmt.Errorf("recording runtime is unknown; use Refresh recording status in Publishing after YouTube finishes processing")
		}
		conflict, err := RecordingScheduleConflictForRecording(update.RecordingID, *update.PublishAt, update.DurationSeconds, occupied)
		if err != nil {
			return err
		}
		if conflict != nil {
			return fmt.Errorf("broadcast at %s overlaps the time already reserved by %s (including the five-minute gap); refresh the schedule or choose another time", update.PublishAt.UTC().Format(time.RFC3339), conflict.Label())
		}
		occupied = append(occupied, RecordingPublishReservation{RecordingID: update.RecordingID, PublishAt: *update.PublishAt, DurationSeconds: update.DurationSeconds})
	}
	return nil
}

func (reservation RecordingPublishReservation) Label() string {
	title := reservation.Title
	if title == "" {
		title = "another recording"
	}
	if reservation.ConferenceTag != "" {
		title += " (" + reservation.ConferenceTag + ")"
	}
	return title
}

func (reservation RecordingPublishReservation) ReservedUntil() time.Time {
	return reservation.PublishAt.Add(time.Duration(reservation.DurationSeconds)*time.Second + RecordingBroadcastGap)
}

// RecordingScheduleConflict uses half-open UTC intervals. The gap applies
// after each recording; starting exactly when it expires is allowed.
func RecordingScheduleConflict(start time.Time, seconds int64, reservations []RecordingPublishReservation) (*RecordingPublishReservation, error) {
	return RecordingScheduleConflictForRecording("", start, seconds, reservations)
}

func RecordingScheduleConflictForRecording(recordingID string, start time.Time, seconds int64, reservations []RecordingPublishReservation) (*RecordingPublishReservation, error) {
	end := start.Add(time.Duration(seconds)*time.Second + RecordingBroadcastGap)
	for _, reservation := range reservations {
		if recordingID != "" && reservation.RecordingID == recordingID {
			continue
		}
		if reservation.RecordingID != "" && reservation.DurationSeconds <= 0 {
			return nil, fmt.Errorf("runtime is unknown for %s; use Refresh recording status in Publishing before scheduling", reservation.Label())
		}
		if start.Before(reservation.ReservedUntil()) && reservation.PublishAt.Before(end) {
			return &reservation, nil
		}
	}
	return nil, nil
}

func CacheRecordingYouTubeDuration(ctx *config.AppContext, recordingID, youtubeURL, fileURI string, seconds int64) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if seconds <= 0 || seconds > 31536000 {
		return fmt.Errorf("invalid recording runtime")
	}
	// Compare the media identity so a slow metadata lookup cannot attach its
	// runtime to a replacement video.
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE recordings SET duration_seconds = $3 WHERE id = $1::uuid AND youtube_url = $2 AND file_uri = $4 AND duration_seconds = 0`, recordingID, youtubeURL, seconds, fileURI)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("recording changed while reading its runtime; refresh the page")
	}
	return nil
}
