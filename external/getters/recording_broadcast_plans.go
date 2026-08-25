package getters

import (
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5/pgtype"
)

type RecordingBroadcastPlanFilter struct {
	ConferenceTag string
	UpdatedAfter  *time.Time
}

// ListRecordingBroadcastPlans returns recordings for which btcpp.dev has
// started the X scheduling workflow. The X operation, recording metadata, and
// stream callback timestamps jointly form the incremental polling cursor.
func ListRecordingBroadcastPlans(ctx *config.AppContext, filter RecordingBroadcastPlanFilter) ([]*types.RecordingBroadcastPlan, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var updatedAfter pgtype.Timestamptz
	if filter.UpdatedAfter != nil {
		updatedAfter = pgtype.Timestamptz{Time: filter.UpdatedAfter.UTC(), Valid: true}
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT r.id::text, c.tag, ct.id::text, r.talk_name, r.file_uri,
			r.publish_at, xb.status, xb.scheduled_at,
			coalesce(rb.x_broadcast_url, ''),
			greatest(r.updated_at, xb.updated_at, coalesce(rb.updated_at, '-infinity'::timestamptz)) AS plan_updated_at
		FROM recording_x_broadcasts xb
		JOIN recordings r ON r.id = xb.recording_id
		JOIN conf_talks ct ON ct.id = r.conf_talk_id
		JOIN conferences c ON c.id = ct.conference_id
		LEFT JOIN recording_broadcasts rb ON rb.recording_id = r.id
		WHERE ($1 = '' OR c.tag = $1)
		  AND ($2::timestamptz IS NULL OR greatest(r.updated_at, xb.updated_at, coalesce(rb.updated_at, '-infinity'::timestamptz)) > $2)
		ORDER BY plan_updated_at, r.id
	`, strings.TrimSpace(filter.ConferenceTag), updatedAfter)
	if err != nil {
		return nil, fmt.Errorf("query recording broadcast plans: %w", err)
	}
	defer rows.Close()

	plans := make([]*types.RecordingBroadcastPlan, 0)
	for rows.Next() {
		var plan types.RecordingBroadcastPlan
		var publishAt pgtype.Timestamptz
		if err := rows.Scan(
			&plan.RecordingID, &plan.ConferenceTag, &plan.TalkID, &plan.Title,
			&plan.SourceObjectKey, &publishAt, &plan.Status, &plan.ScheduledAt,
			&plan.XBroadcastURL, &plan.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recording broadcast plan: %w", err)
		}
		plan.PublishAt = pgTimestampPtr(publishAt)
		plans = append(plans, &plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recording broadcast plans: %w", err)
	}
	return plans, nil
}
