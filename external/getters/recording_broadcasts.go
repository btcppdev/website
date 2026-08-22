package getters

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type RecordingBroadcastUpdate struct {
	State         string
	HLSURL        string
	XBroadcastURL string
	Now           time.Time
}

func GetRecordingBroadcast(ctx *config.AppContext, recordingID string) (*types.RecordingBroadcast, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT recording_id::text, state, hls_url, x_broadcast_url,
			started_at, ended_at, heartbeat_at, updated_at
		FROM recording_broadcasts WHERE recording_id = $1::uuid
	`, strings.TrimSpace(recordingID))
	var out types.RecordingBroadcast
	var started, ended, heartbeat pgtype.Timestamptz
	if err := row.Scan(&out.RecordingID, &out.State, &out.HLSURL, &out.XBroadcastURL, &started, &ended, &heartbeat, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get recording broadcast: %w", err)
	}
	out.StartedAt = pgTimestampPtr(started)
	out.EndedAt = pgTimestampPtr(ended)
	out.HeartbeatAt = pgTimestampPtr(heartbeat)
	return &out, nil
}

// GetActiveRecordingBroadcast returns the freshest live broadcast whose
// heartbeat is newer than cutoff. A stale broadcaster therefore disappears
// from the site-wide live indicator without requiring an explicit stop call.
func GetActiveRecordingBroadcast(ctx *config.AppContext, cutoff time.Time) (*types.RecordingBroadcast, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT recording_id::text, state, hls_url, x_broadcast_url,
			started_at, ended_at, heartbeat_at, updated_at
		FROM recording_broadcasts
		WHERE state = 'live' AND heartbeat_at > $1::timestamptz
		ORDER BY heartbeat_at DESC
		LIMIT 1
	`, cutoff)
	var out types.RecordingBroadcast
	var started, ended, heartbeat pgtype.Timestamptz
	if err := row.Scan(&out.RecordingID, &out.State, &out.HLSURL, &out.XBroadcastURL, &started, &ended, &heartbeat, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active recording broadcast: %w", err)
	}
	out.StartedAt = pgTimestampPtr(started)
	out.EndedAt = pgTimestampPtr(ended)
	out.HeartbeatAt = pgTimestampPtr(heartbeat)
	return &out, nil
}

func UpsertRecordingBroadcast(ctx *config.AppContext, recordingID string, update RecordingBroadcastUpdate) (*types.RecordingBroadcast, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	recordingID = strings.TrimSpace(recordingID)
	state := strings.ToLower(strings.TrimSpace(update.State))
	if state != "scheduled" && state != "live" && state != "ended" && state != "failed" {
		return nil, fmt.Errorf("invalid broadcast state %q", state)
	}
	now := update.Now
	if now.IsZero() {
		now = time.Now()
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO recording_broadcasts (
			recording_id, state, hls_url, x_broadcast_url,
			started_at, ended_at, heartbeat_at, updated_at
		) VALUES (
			$1::uuid, $2, $3, $4,
			CASE WHEN $2 = 'live' THEN $5::timestamptz END,
			CASE WHEN $2 IN ('ended', 'failed') THEN $5::timestamptz END,
			CASE WHEN $2 = 'live' THEN $5::timestamptz END,
			$5::timestamptz
		)
		ON CONFLICT (recording_id) DO UPDATE SET
			state = EXCLUDED.state,
			hls_url = EXCLUDED.hls_url,
			x_broadcast_url = EXCLUDED.x_broadcast_url,
			started_at = CASE
				WHEN EXCLUDED.state = 'live' THEN coalesce(recording_broadcasts.started_at, EXCLUDED.started_at)
				ELSE recording_broadcasts.started_at
			END,
			ended_at = CASE
				WHEN EXCLUDED.state IN ('ended', 'failed') THEN EXCLUDED.ended_at
				WHEN EXCLUDED.state = 'live' THEN NULL
				ELSE recording_broadcasts.ended_at
			END,
			heartbeat_at = CASE WHEN EXCLUDED.state = 'live' THEN EXCLUDED.heartbeat_at ELSE recording_broadcasts.heartbeat_at END,
			updated_at = EXCLUDED.updated_at
	`, recordingID, state, strings.TrimSpace(update.HLSURL), strings.TrimSpace(update.XBroadcastURL), now)
	if err != nil {
		return nil, fmt.Errorf("upsert recording broadcast: %w", err)
	}
	return GetRecordingBroadcast(ctx, recordingID)
}

func pgTimestampPtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
