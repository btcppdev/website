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

type ConferenceBroadcastUpdate struct {
	Title         string
	State         string
	HLSURL        string
	XBroadcastURL string
	Now           time.Time
}

func GetConferenceBroadcast(ctx *config.AppContext, conferenceID string) (*types.ConferenceBroadcast, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT conference_id::text, title, state, hls_url, x_broadcast_url,
			started_at, ended_at, heartbeat_at, updated_at
		FROM conference_broadcasts WHERE conference_id = $1::uuid
	`, strings.TrimSpace(conferenceID))
	var out types.ConferenceBroadcast
	var started, ended, heartbeat pgtype.Timestamptz
	if err := row.Scan(&out.ConferenceID, &out.Title, &out.State, &out.HLSURL, &out.XBroadcastURL, &started, &ended, &heartbeat, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conference broadcast: %w", err)
	}
	out.StartedAt = pgTimestampPtr(started)
	out.EndedAt = pgTimestampPtr(ended)
	out.HeartbeatAt = pgTimestampPtr(heartbeat)
	return &out, nil
}

// GetActiveConferenceBroadcast returns the freshest live broadcast whose
// heartbeat is newer than cutoff. A stale broadcaster therefore disappears
// from the site-wide live indicator without requiring an explicit stop call.
func GetActiveConferenceBroadcast(ctx *config.AppContext, cutoff time.Time) (*types.ConferenceBroadcast, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT conference_id::text, title, state, hls_url, x_broadcast_url,
			started_at, ended_at, heartbeat_at, updated_at
		FROM conference_broadcasts
		WHERE state = 'live' AND heartbeat_at > $1::timestamptz
			AND conference_id IN (SELECT id FROM conferences WHERE publication_status = 'published')
		ORDER BY heartbeat_at DESC
		LIMIT 1
	`, cutoff)
	var out types.ConferenceBroadcast
	var started, ended, heartbeat pgtype.Timestamptz
	if err := row.Scan(&out.ConferenceID, &out.Title, &out.State, &out.HLSURL, &out.XBroadcastURL, &started, &ended, &heartbeat, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active conference broadcast: %w", err)
	}
	out.StartedAt = pgTimestampPtr(started)
	out.EndedAt = pgTimestampPtr(ended)
	out.HeartbeatAt = pgTimestampPtr(heartbeat)
	return &out, nil
}

func UpsertConferenceBroadcast(ctx *config.AppContext, conferenceID string, update ConferenceBroadcastUpdate) (*types.ConferenceBroadcast, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	conferenceID = strings.TrimSpace(conferenceID)
	state := strings.ToLower(strings.TrimSpace(update.State))
	if state != "scheduled" && state != "live" && state != "ended" && state != "failed" {
		return nil, fmt.Errorf("invalid broadcast state %q", state)
	}
	now := update.Now
	if now.IsZero() {
		now = time.Now()
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO conference_broadcasts (
			conference_id, title, state, hls_url, x_broadcast_url,
			started_at, ended_at, heartbeat_at, updated_at
		) VALUES (
			$1::uuid, $6, $2, $3, $4,
			CASE WHEN $2 = 'live' THEN $5::timestamptz END,
			CASE WHEN $2 IN ('ended', 'failed') THEN $5::timestamptz END,
			CASE WHEN $2 = 'live' THEN $5::timestamptz END,
			$5::timestamptz
		)
		ON CONFLICT (conference_id) DO UPDATE SET
			state = EXCLUDED.state,
			title = EXCLUDED.title,
			hls_url = EXCLUDED.hls_url,
			x_broadcast_url = CASE
				WHEN EXCLUDED.x_broadcast_url <> '' THEN EXCLUDED.x_broadcast_url
				ELSE conference_broadcasts.x_broadcast_url
			END,
			started_at = CASE
				WHEN EXCLUDED.state = 'live' AND conference_broadcasts.state <> 'live' THEN EXCLUDED.started_at
				WHEN EXCLUDED.state = 'live' THEN coalesce(conference_broadcasts.started_at, EXCLUDED.started_at)
				ELSE conference_broadcasts.started_at
			END,
			ended_at = CASE
				WHEN EXCLUDED.state IN ('ended', 'failed') THEN EXCLUDED.ended_at
				WHEN EXCLUDED.state = 'live' THEN NULL
				ELSE conference_broadcasts.ended_at
			END,
			heartbeat_at = CASE WHEN EXCLUDED.state = 'live' THEN EXCLUDED.heartbeat_at ELSE conference_broadcasts.heartbeat_at END,
			updated_at = EXCLUDED.updated_at
	`, conferenceID, state, strings.TrimSpace(update.HLSURL), strings.TrimSpace(update.XBroadcastURL), now, strings.TrimSpace(update.Title))
	if err != nil {
		return nil, fmt.Errorf("upsert conference broadcast: %w", err)
	}
	return GetConferenceBroadcast(ctx, conferenceID)
}
