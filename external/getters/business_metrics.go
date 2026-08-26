package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// ListBusinessMetricCounts returns privacy-safe, bounded aggregates for the
// Prometheus business collector. Identifiers never leave this query.
func ListBusinessMetricCounts(ctx *config.AppContext) ([]types.BusinessMetricCount, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT 'tickets', coalesce(c.tag, 'unassigned'),
			CASE WHEN r.revoked THEN 'revoked' ELSE 'active' END, count(*)::float8
		FROM registrations r
		LEFT JOIN conferences c ON c.id = r.conference_id
		GROUP BY coalesce(c.tag, 'unassigned'), CASE WHEN r.revoked THEN 'revoked' ELSE 'active' END

		UNION ALL

		SELECT 'ticket_checkins', coalesce(c.tag, 'unassigned'), '', count(*)::float8
		FROM registrations r
		LEFT JOIN conferences c ON c.id = r.conference_id
		WHERE r.checked_in_at IS NOT NULL AND NOT r.revoked
		GROUP BY coalesce(c.tag, 'unassigned')

		UNION ALL

		SELECT 'speaker_applications', coalesce(c.tag, 'unassigned'),
			CASE
				WHEN p.status IN ('', 'Applied', 'InReview') THEN 'pending'
				WHEN p.status = 'Invited' THEN 'invited'
				WHEN p.status IN ('Accepted', 'Scheduled') THEN 'accepted'
				WHEN p.status = 'Waitlisted' THEN 'waitlisted'
				WHEN p.status IN ('TheyDecline', 'WeDecline', 'Rejected', 'Declined') THEN 'declined'
				ELSE 'other'
			END, count(*)::float8
		FROM proposals p
		LEFT JOIN conferences c ON c.id = p.conference_id
		GROUP BY coalesce(c.tag, 'unassigned'), CASE
			WHEN p.status IN ('', 'Applied', 'InReview') THEN 'pending'
			WHEN p.status = 'Invited' THEN 'invited'
			WHEN p.status IN ('Accepted', 'Scheduled') THEN 'accepted'
			WHEN p.status = 'Waitlisted' THEN 'waitlisted'
			WHEN p.status IN ('TheyDecline', 'WeDecline', 'Rejected', 'Declined') THEN 'declined'
			ELSE 'other'
		END

		UNION ALL

		SELECT 'volunteer_applications', c.tag,
			CASE
				WHEN v.status = 'Applied' THEN 'applied'
				WHEN v.status = 'Waitlist' THEN 'waitlisted'
				WHEN v.status = 'PendingShifts' THEN 'pending_shifts'
				WHEN v.status = 'Scheduled' THEN 'scheduled'
				WHEN v.status = 'Declined' THEN 'declined'
				ELSE 'other'
			END, count(*)::float8
		FROM volunteers v
		JOIN volunteers_conferences vc ON vc.volunteer_id = v.id AND vc.kind = 'schedule_for'
		JOIN conferences c ON c.id = vc.conference_id
		GROUP BY c.tag, CASE
			WHEN v.status = 'Applied' THEN 'applied'
			WHEN v.status = 'Waitlist' THEN 'waitlisted'
			WHEN v.status = 'PendingShifts' THEN 'pending_shifts'
			WHEN v.status = 'Scheduled' THEN 'scheduled'
			WHEN v.status = 'Declined' THEN 'declined'
			ELSE 'other'
		END

		UNION ALL

		SELECT 'recording_broadcasts', c.tag, rb.state, count(*)::float8
		FROM recording_broadcasts rb
		JOIN recordings r ON r.id = rb.recording_id
		JOIN conf_talks ct ON ct.id = r.conf_talk_id
		JOIN conferences c ON c.id = ct.conference_id
		GROUP BY c.tag, rb.state

		ORDER BY 1, 2, 3
	`)
	if err != nil {
		return nil, fmt.Errorf("query business metrics: %w", err)
	}
	defer rows.Close()

	counts := make([]types.BusinessMetricCount, 0)
	for rows.Next() {
		var count types.BusinessMetricCount
		if err := rows.Scan(&count.Metric, &count.Conference, &count.State, &count.Count); err != nil {
			return nil, fmt.Errorf("scan business metric: %w", err)
		}
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate business metrics: %w", err)
	}
	return counts, nil
}
