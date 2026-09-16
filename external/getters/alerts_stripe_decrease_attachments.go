package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

type alertsDecreaseAttachmentClaim struct {
	Binding      alertsstripe.Binding
	Quote        AlertsStripeDecreaseQuote
	Attempt      int64
	ScheduleID   string
	FirstAttempt time.Time
}

// Commit the lease before provider I/O. Keep ambiguous attempts pending even
// when their review expires; expiry is not evidence that Stripe did nothing.
func claimAlertsStripeDecreaseAttachment(ctx context.Context, app *config.AppContext, person, id string) (*alertsDecreaseAttachmentClaim, error) {
	q, err := AlertsStripeDecreaseQuoteForPerson(ctx, app, person, id)
	if err != nil {
		return nil, err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return nil, err
	}
	var raw, snapshot []byte
	var state string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT b.binding,d.state,s.snapshot,s.revision FROM alerts_stripe_decreases d JOIN alerts_stripe_checkouts c ON c.id=d.checkout_id JOIN alerts_stripe_current_subscriptions b ON b.checkout_id=c.id JOIN alerts_stripe_subscription_state s ON s.checkout_id=c.id WHERE d.quote_id=$1 AND c.person_id=$2 AND c.closed_at IS NULL FOR UPDATE OF c,d`, id, person).Scan(&raw, &state, &snapshot, &revision)
	if err != nil {
		return nil, err
	}
	if state != "pending" {
		return nil, alertsstripe.ErrReconcile
	}
	out := &alertsDecreaseAttachmentClaim{Quote: *q}
	if json.Unmarshal(raw, &out.Binding) != nil || out.Binding.PersonID != person || out.Binding.IntentID != q.CheckoutID {
		return nil, alertsstripe.ErrSubscription
	}
	var lease time.Time
	err = tx.QueryRow(ctx, `SELECT attempts,lease_until,COALESCE(schedule_id,''),first_attempt_at FROM alerts_stripe_decrease_attachments WHERE quote_id=$1`, id).Scan(&out.Attempt, &lease, &out.ScheduleID, &out.FirstAttempt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if out.ScheduleID != "" {
		return out, tx.Commit(ctx)
	}
	now := time.Now().UTC()
	if !now.Before(q.Preview.ExpiresAt) && out.Attempt == 0 {
		if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='expired',updated_at=now() WHERE quote_id=$1`, id); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, alertsstripe.ErrReconcile
	}
	if out.Attempt > 0 && (out.FirstAttempt.After(now) || !out.FirstAttempt.Before(q.Preview.ExpiresAt) || !now.Before(out.FirstAttempt.Add(23*time.Hour)) || now.Unix() >= q.Preview.EffectiveAt) {
		if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='review',updated_at=now() WHERE quote_id=$1`, id); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, alertsstripe.ErrReconcile
	}
	if lease.After(now) {
		return nil, alertsstripe.ErrReconcile
	}
	var s alertsstripe.SubscriptionState
	if json.Unmarshal(snapshot, &s) != nil || (out.Attempt == 0 && revision != q.Revision) || out.Binding.PaidSlots != q.Preview.PreviousSlots || s.State != "active" || s.ProviderStatus != "active" || s.CancelAtPeriodEnd || s.CancelAt != 0 || s.CollectionPaused || s.PeriodStart != q.Preview.PeriodStart || s.PeriodEnd != q.Preview.EffectiveAt {
		return nil, alertsstripe.ErrSubscription
	}
	err = tx.QueryRow(ctx, `INSERT INTO alerts_stripe_decrease_attachments(quote_id,lease_until) VALUES($1,now()+interval '2 minutes') ON CONFLICT(quote_id) DO UPDATE SET attempts=alerts_stripe_decrease_attachments.attempts+1,lease_until=excluded.lease_until RETURNING attempts,first_attempt_at`, id).Scan(&out.Attempt, &out.FirstAttempt)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// AttachAlertsStripeDecrease persists the verified schedule reference. This
// step does not change quantities, grant access, or mark the decrease scheduled.
func AttachAlertsStripeDecrease(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (string, error) {
	claim, err := claimAlertsStripeDecreaseAttachment(ctx, app, person, id)
	if err != nil {
		return "", err
	}
	if claim.ScheduleID != "" {
		return claim.ScheduleID, nil
	}
	schedule, err := client.ResumeDecreaseSchedule(ctx, claim.Binding, id, claim.Quote.Preview, claim.FirstAttempt, time.Now().UTC())
	if err != nil {
		return "", err
	}
	updated, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_attachments SET schedule_id=$3 WHERE quote_id=$1 AND attempts=$2 AND (schedule_id IS NULL OR schedule_id=$3)`, id, claim.Attempt, schedule)
	if err != nil {
		return "", err
	}
	if updated.RowsAffected() != 1 {
		return "", alertsstripe.ErrReconcile
	}
	return schedule, nil
}
