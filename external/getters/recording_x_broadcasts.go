package getters

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

const xBroadcastOperationLease = 15 * time.Minute

// ClaimRecordingXBroadcastStep atomically claims the next incomplete step.
// Stable IDs determine recovery after a failed or abandoned operation, so a
// retry never calls create again after X has returned a broadcast ID.
func ClaimRecordingXBroadcastStep(ctx *config.AppContext, recordingID string, scheduledAt time.Time, sessionID, optimisticPosterURL string, now time.Time) (*types.RecordingXBroadcast, string, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, "", errors.New("database is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, "", fmt.Errorf("begin X broadcast claim: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	row, err := scanRecordingXBroadcast(tx.QueryRow(ctx.DatabaseContext(), `
		SELECT recording_id::text, status, scheduled_at, scheduled_broadcast_id,
			broadcast_id, poster_media_id, poster_url, x_session_id,
			optimistic_poster_url, error, operation_started_at, created_at, updated_at
		FROM recording_x_broadcasts
		WHERE recording_id = $1::uuid
		FOR UPDATE
	`, strings.TrimSpace(recordingID)))
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = scanRecordingXBroadcast(tx.QueryRow(ctx.DatabaseContext(), `
			INSERT INTO recording_x_broadcasts (
				recording_id, status, scheduled_at, x_session_id,
				optimistic_poster_url, operation_started_at, created_at, updated_at
			) VALUES ($1::uuid, 'creating', $2, $3, $4, $5, $5, $5)
			RETURNING recording_id::text, status, scheduled_at, scheduled_broadcast_id,
				broadcast_id, poster_media_id, poster_url, x_session_id,
				optimistic_poster_url, error, operation_started_at, created_at, updated_at
		`, recordingID, scheduledAt, sessionID, optimisticPosterURL, now))
		if err != nil {
			return nil, "", fmt.Errorf("create X broadcast operation: %w", err)
		}
		if err := tx.Commit(ctx.DatabaseContext()); err != nil {
			return nil, "", fmt.Errorf("commit X broadcast claim: %w", err)
		}
		return row, "create", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("load X broadcast operation: %w", err)
	}
	if row.Status == "scheduled" && row.ScheduledAt.Equal(scheduledAt) {
		return row, "", nil
	}
	active := row.Status == "creating" || row.Status == "uploading_poster" || row.Status == "finalizing"
	if active && row.OperationStartedAt.After(now.Add(-xBroadcastOperationLease)) {
		return row, "", nil
	}

	action, status := "create", "creating"
	if row.BroadcastID != "" {
		action, status = "upload_poster", "uploading_poster"
	}
	if row.PosterMediaID != "" {
		action, status = "finalize", "finalizing"
	}
	row, err = scanRecordingXBroadcast(tx.QueryRow(ctx.DatabaseContext(), `
		UPDATE recording_x_broadcasts
		SET status = $2, scheduled_at = $3, error = '', operation_started_at = $4, updated_at = $4
		WHERE recording_id = $1::uuid
		RETURNING recording_id::text, status, scheduled_at, scheduled_broadcast_id,
			broadcast_id, poster_media_id, poster_url, x_session_id,
			optimistic_poster_url, error, operation_started_at, created_at, updated_at
	`, recordingID, status, scheduledAt, now))
	if err != nil {
		return nil, "", fmt.Errorf("claim X broadcast step: %w", err)
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, "", fmt.Errorf("commit X broadcast claim: %w", err)
	}
	return row, action, nil
}

func GetRecordingXBroadcast(ctx *config.AppContext, recordingID string) (*types.RecordingXBroadcast, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, errors.New("database is not configured")
	}
	row, err := scanRecordingXBroadcast(ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT recording_id::text, status, scheduled_at, scheduled_broadcast_id,
			broadcast_id, poster_media_id, poster_url, x_session_id,
			optimistic_poster_url, error, operation_started_at, created_at, updated_at
		FROM recording_x_broadcasts WHERE recording_id = $1::uuid
	`, strings.TrimSpace(recordingID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get recording X broadcast: %w", err)
	}
	return row, nil
}

func ListRecordingXBroadcasts(ctx *config.AppContext, recordingIDs []string) (map[string]*types.RecordingXBroadcast, error) {
	out := make(map[string]*types.RecordingXBroadcast)
	if len(recordingIDs) == 0 {
		return out, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT recording_id::text, status, scheduled_at, scheduled_broadcast_id,
			broadcast_id, poster_media_id, poster_url, x_session_id,
			optimistic_poster_url, error, operation_started_at, created_at, updated_at
		FROM recording_x_broadcasts WHERE recording_id = ANY($1::uuid[])
	`, recordingIDs)
	if err != nil {
		return nil, fmt.Errorf("list recording X broadcasts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		broadcast, err := scanRecordingXBroadcast(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recording X broadcast: %w", err)
		}
		out[broadcast.RecordingID] = broadcast
	}
	return out, rows.Err()
}

func SaveRecordingXBroadcastCreated(ctx *config.AppContext, recordingID, scheduledID, broadcastID string, now time.Time) error {
	return updateRecordingXBroadcastStep(ctx, recordingID, "created", now, `scheduled_broadcast_id = $4, broadcast_id = $5`, scheduledID, broadcastID)
}

func SaveRecordingXBroadcastPoster(ctx *config.AppContext, recordingID, posterMediaID string, now time.Time) error {
	return updateRecordingXBroadcastStep(ctx, recordingID, "poster_uploaded", now, `poster_media_id = $4`, posterMediaID)
}

func SaveRecordingXBroadcastFinalized(ctx *config.AppContext, recordingID, posterURL string, now time.Time) error {
	return updateRecordingXBroadcastStep(ctx, recordingID, "scheduled", now, `poster_url = $4`, posterURL)
}

func FailRecordingXBroadcast(ctx *config.AppContext, recordingID, message string, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE recording_x_broadcasts SET status = 'failed', error = $2, updated_at = $3
		WHERE recording_id = $1::uuid
	`, recordingID, strings.TrimSpace(message), now)
	if err != nil {
		return fmt.Errorf("save X broadcast failure: %w", err)
	}
	return nil
}

func updateRecordingXBroadcastStep(ctx *config.AppContext, recordingID, status string, now time.Time, assignment string, args ...any) error {
	if now.IsZero() {
		now = time.Now()
	}
	query := `UPDATE recording_x_broadcasts SET status = $2, error = '', updated_at = $3, ` + assignment + ` WHERE recording_id = $1::uuid`
	params := []any{recordingID, status, now}
	params = append(params, args...)
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), query, params...)
	if err != nil {
		return fmt.Errorf("save X broadcast %s: %w", status, err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("X broadcast operation disappeared while saving progress")
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanRecordingXBroadcast(row rowScanner) (*types.RecordingXBroadcast, error) {
	var out types.RecordingXBroadcast
	err := row.Scan(
		&out.RecordingID, &out.Status, &out.ScheduledAt,
		&out.ScheduledBroadcastID, &out.BroadcastID, &out.PosterMediaID,
		&out.PosterURL, &out.SessionID, &out.OptimisticPosterURL, &out.Error,
		&out.OperationStartedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	return &out, err
}
