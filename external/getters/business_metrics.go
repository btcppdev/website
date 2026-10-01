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

		UNION ALL

		SELECT 'service_receipt_jobs', '', states.state, count(j.order_id)::float8
		FROM (VALUES ('pending'), ('accepted'), ('cancelled'), ('review')) states(state)
		LEFT JOIN service_receipt_jobs j ON j.state=states.state
		GROUP BY states.state

		UNION ALL

		SELECT 'service_receipt_oldest_pending_timestamp_seconds', '', '',
			coalesce(extract(epoch FROM min(created_at)), 0)::float8
		FROM service_receipt_jobs WHERE state='pending'

        UNION ALL
        SELECT 'service_fulfillment_jobs', '', states.state, count(j.attempt_id)::float8
        FROM (VALUES ('pending'), ('accepted'), ('review')) states(state)
        LEFT JOIN service_fulfillment_jobs j ON j.state=states.state
        GROUP BY states.state
        UNION ALL
        SELECT 'service_fulfillment_oldest_pending_timestamp_seconds', '', '',
            coalesce(extract(epoch FROM min(created_at)),0)::float8
        FROM service_fulfillment_jobs WHERE state='pending'        UNION ALL
        SELECT 'service_refund_jobs', '', states.state, count(j.attempt_id)::float8
        FROM (VALUES ('pending'), ('accepted'), ('review')) states(state)
        LEFT JOIN service_refund_jobs j ON j.state=states.state
        GROUP BY states.state
        UNION ALL
        SELECT 'service_refund_oldest_pending_timestamp_seconds', '', '',
            coalesce(extract(epoch FROM min(created_at)),0)::float8
        FROM service_refund_jobs WHERE state='pending'

 UNION ALL
 SELECT 'service_refund_tax_jobs', '', states.state, count(j.refund_reference)::float8
 FROM (VALUES ('pending'), ('accepted'), ('review')) states(state)
 LEFT JOIN service_refund_tax_jobs j ON j.state=states.state GROUP BY states.state
 UNION ALL
 SELECT 'service_refund_tax_oldest_pending_timestamp_seconds', '', '',
 coalesce(extract(epoch FROM min(created_at)),0)::float8
 FROM service_refund_tax_jobs WHERE state='pending'

 UNION ALL
 SELECT 'account_deletion_delivery_jobs', '', states.state, count(j.person_id)::float8
 FROM (VALUES ('pending'), ('accepted')) states(state)
 LEFT JOIN account_deletion_deliveries j ON j.state=states.state GROUP BY states.state
 UNION ALL
 SELECT 'account_deletion_delivery_oldest_pending_timestamp_seconds', '', '', coalesce(extract(epoch FROM min(created_at)),0)::float8 FROM account_deletion_deliveries WHERE state='pending'

 UNION ALL
 SELECT 'account_deletion_cancellation_jobs', '', states.state, count(j.checkout_id)::float8
 FROM (VALUES ('pending'), ('confirmed'), ('review')) states(state)
 LEFT JOIN account_deletion_cancellations j ON j.state=states.state GROUP BY states.state
 UNION ALL
 SELECT 'account_deletion_cancellation_oldest_pending_timestamp_seconds', '', '', coalesce(extract(epoch FROM min(requested_at)),0)::float8 FROM account_deletion_cancellations WHERE state='pending'

 UNION ALL
 SELECT 'alerts_checkout_recovery_jobs','',states.state,count(c.id)::float8
 FROM (VALUES ('missing_reference'),('referenced'),('bound')) states(state)
 LEFT JOIN alerts_stripe_checkouts c ON c.person_id IS NOT NULL AND c.closed_at IS NULL
 AND states.state=CASE WHEN EXISTS(SELECT 1 FROM alerts_stripe_subscriptions b WHERE b.checkout_id=c.id) THEN 'bound'
 WHEN c.stripe_session_id IS NULL THEN 'missing_reference' ELSE 'referenced' END
 GROUP BY states.state
 UNION ALL
 SELECT 'alerts_checkout_oldest_due_timestamp_seconds','','',coalesce(extract(epoch FROM min(next_reconcile_at)),0)::float8
 FROM alerts_stripe_checkouts WHERE person_id IS NOT NULL AND closed_at IS NULL
 UNION ALL
 SELECT 'alerts_checkout_oldest_unresolved_timestamp_seconds','','',coalesce(extract(epoch FROM min(c.created_at)),0)::float8
 FROM alerts_stripe_checkouts c WHERE c.person_id IS NOT NULL AND c.closed_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM alerts_stripe_subscriptions b WHERE b.checkout_id=c.id)
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
