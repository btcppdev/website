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

// ReconcileAlertsStripeDecrease observes the renewal boundary without changing
// Stripe. A quantity transition is not a payment and creates no entitlement.
func ReconcileAlertsStripeDecrease(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	return reconcileAlertsStripeDecreaseAt(ctx, app, person, id, client, time.Now().UTC())
}

func reconcileAlertsStripeDecreaseAt(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client, now time.Time) error {
	q, err := AlertsStripeDecreaseQuoteForPerson(ctx, app, person, id)
	if err != nil {
		return err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	var raw []byte
	var status, schedule string
	err = tx.QueryRow(ctx, `SELECT b.binding,d.state,s.schedule_id FROM alerts_stripe_decreases d JOIN alerts_stripe_checkouts c ON c.id=d.checkout_id JOIN alerts_stripe_current_subscriptions b ON b.checkout_id=c.id JOIN alerts_stripe_decrease_schedules s ON s.quote_id=d.quote_id WHERE d.quote_id=$1 AND c.person_id=$2 AND s.snapshot IS NOT NULL FOR UPDATE OF c,d`, id, person).Scan(&raw, &status, &schedule)
	if err != nil {
		return err
	}
	if status == "applied" {
		return tx.Commit(ctx)
	}
	if status != "scheduled" {
		return alertsstripe.ErrReconcile
	}
	if now.Unix() < q.Preview.EffectiveAt {
		return tx.Commit(ctx)
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != q.CheckoutID {
		return alertsstripe.ErrSubscription
	}
	observed, err := client.ObserveDecreaseTransition(ctx, b, id, schedule, q.Preview, now)
	if err != nil {
		return err
	}
	if err = recordAlertsStripeDecreaseTransition(ctx, tx, *q, b, *observed, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Caller holds the account lock and supplies authenticated adapter evidence.
func recordAlertsStripeDecreaseTransition(ctx context.Context, tx pgx.Tx, q AlertsStripeDecreaseQuote, b alertsstripe.Binding, observed alertsstripe.DecreaseTransition, now time.Time) error {
	expected := b
	expected.PaidSlots = q.Preview.PaidSlots
	if observed.Binding != expected || b.PaidSlots != q.Preview.PreviousSlots || b.IntentID != q.CheckoutID || observed.EffectiveAt != q.Preview.EffectiveAt || now.Unix() < observed.EffectiveAt || observed.State.PeriodStart < observed.EffectiveAt || observed.State.PeriodStart > now.Unix() || observed.State.PeriodEnd <= observed.State.PeriodStart {
		return alertsstripe.ErrSubscription
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_decrease_transitions WHERE quote_id=$1)`, q.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	rawBinding, err := json.Marshal(observed.Binding)
	if err != nil {
		return err
	}
	rawTransition, err := json.Marshal(observed)
	if err != nil {
		return err
	}
	// Tie the observation to the immutable verified schedule, not a supplied ID.
	inserted, err := tx.Exec(ctx, `INSERT INTO alerts_stripe_decrease_transitions(quote_id,checkout_id,binding,transition,effective_at) SELECT d.quote_id,d.checkout_id,$3,$4,$5 FROM alerts_stripe_decreases d JOIN alerts_stripe_decrease_schedules s ON s.quote_id=d.quote_id WHERE d.quote_id=$1 AND s.schedule_id=$2 AND s.snapshot IS NOT NULL AND d.state='scheduled'`, q.ID, observed.ScheduleID, rawBinding, rawTransition, time.Unix(observed.EffectiveAt, 0))
	if err != nil {
		return err
	}
	if inserted.RowsAffected() != 1 {
		return alertsstripe.ErrReconcile
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='applied',updated_at=now() WHERE quote_id=$1`, q.ID); err != nil {
		return err
	}
	rawState, err := json.Marshal(observed.State)
	if err != nil {
		return err
	}
	updated, err := tx.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=revision+1,snapshot=$2,observed_at=now() WHERE checkout_id=$1`, q.CheckoutID, rawState)
	if err != nil {
		return err
	}
	if updated.RowsAffected() != 1 {
		return errors.New("missing subscription state for decrease")
	}
	return nil
}
