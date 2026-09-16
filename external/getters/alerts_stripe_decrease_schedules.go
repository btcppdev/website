package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
	"time"
)

type alertsDecreaseScheduleClaim struct {
	Binding      alertsstripe.Binding
	Quote        AlertsStripeDecreaseQuote
	ScheduleID   string
	FirstAttempt time.Time
	Attempt      int64
	Complete     bool
}

func claimAlertsStripeDecreaseSchedule(ctx context.Context, app *config.AppContext, person, id string) (*alertsDecreaseScheduleClaim, error) {
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
	out := &alertsDecreaseScheduleClaim{Quote: *q}
	var raw []byte
	var state string
	err = tx.QueryRow(ctx, `SELECT b.binding,d.state,a.schedule_id,a.first_attempt_at FROM alerts_stripe_decreases d JOIN alerts_stripe_checkouts c ON c.id=d.checkout_id JOIN alerts_stripe_current_subscriptions b ON b.checkout_id=c.id JOIN alerts_stripe_decrease_attachments a ON a.quote_id=d.quote_id WHERE d.quote_id=$1 AND c.person_id=$2 AND c.closed_at IS NULL AND a.schedule_id IS NOT NULL FOR UPDATE OF c,d`, id, person).Scan(&raw, &state, &out.ScheduleID, &out.FirstAttempt)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &out.Binding) != nil || out.Binding.PersonID != person || out.Binding.IntentID != q.CheckoutID {
		return nil, alertsstripe.ErrSubscription
	}
	var lease time.Time
	err = tx.QueryRow(ctx, `SELECT attempts,lease_until,snapshot IS NOT NULL FROM alerts_stripe_decrease_schedules WHERE quote_id=$1`, id).Scan(&out.Attempt, &lease, &out.Complete)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if out.Complete && (state == "scheduled" || state == "applied") {
		return out, tx.Commit(ctx)
	}
	if state != "pending" || out.Complete || lease.After(time.Now()) || out.Binding.PaidSlots != q.Preview.PreviousSlots {
		return nil, alertsstripe.ErrReconcile
	}
	err = tx.QueryRow(ctx, `INSERT INTO alerts_stripe_decrease_schedules(quote_id,schedule_id,lease_until) VALUES($1,$2,now()+interval '2 minutes') ON CONFLICT(quote_id) DO UPDATE SET attempts=alerts_stripe_decrease_schedules.attempts+1,lease_until=excluded.lease_until RETURNING attempts`, id, out.ScheduleID).Scan(&out.Attempt)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// Only the verified adapter result may reach this function. Saving provider
// evidence, marking scheduled and invalidating older reviews is one transaction.
func recordAlertsStripeDecreaseSchedule(ctx context.Context, app *config.AppContext, claim *alertsDecreaseScheduleClaim, s *stripe.SubscriptionSchedule) error {
	if err := alertsstripe.VerifyDecreaseSchedule(s, claim.Binding, claim.Quote.ID, claim.ScheduleID, claim.Quote.Preview); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+claim.Binding.PersonID); err != nil {
		return err
	}
	var state string
	var attempt int64
	var complete bool
	err = tx.QueryRow(ctx, `SELECT d.state,s.attempts,s.snapshot IS NOT NULL FROM alerts_stripe_decreases d JOIN alerts_stripe_decrease_schedules s ON s.quote_id=d.quote_id WHERE d.quote_id=$1 AND s.schedule_id=$2 FOR UPDATE OF d,s`, claim.Quote.ID, claim.ScheduleID).Scan(&state, &attempt, &complete)
	if err != nil {
		return err
	}
	if complete && (state == "scheduled" || state == "applied") {
		return tx.Commit(ctx)
	}
	if state != "pending" || attempt != claim.Attempt || complete {
		return alertsstripe.ErrReconcile
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_decrease_schedules SET snapshot=$2,verified_at=now() WHERE quote_id=$1`, claim.Quote.ID, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='scheduled',updated_at=now() WHERE quote_id=$1`, claim.Quote.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=revision+1 WHERE checkout_id=$1`, claim.Quote.CheckoutID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ScheduleAlertsStripeDecrease operates on a saved attachment and confirmation.
// It deliberately leaves paid grants and the effective current quantity intact.
func ScheduleAlertsStripeDecrease(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	claim, err := claimAlertsStripeDecreaseSchedule(ctx, app, person, id)
	if err != nil {
		return err
	}
	if claim.Complete {
		return nil
	}
	result, err := client.SetDecreaseSchedule(ctx, claim.Binding, id, claim.ScheduleID, claim.Quote.Preview, claim.FirstAttempt, time.Now().UTC())
	if err != nil {
		return err
	}
	return recordAlertsStripeDecreaseSchedule(ctx, app, claim, result)
}
