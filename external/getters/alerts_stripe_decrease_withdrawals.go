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

type alertsDecreaseWithdrawal struct {
	ID, State, ScheduleID string
	CreatedAt             time.Time
	Attempt               int64
	Binding               alertsstripe.Binding
	Quote                 AlertsStripeDecreaseQuote
}

func ReserveAlertsStripeDecreaseWithdrawal(ctx context.Context, app *config.AppContext, person, quoteID string) (string, error) {
	q, err := AlertsStripeDecreaseQuoteForPerson(ctx, app, person, quoteID)
	if err != nil {
		return "", err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return "", err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT state FROM alerts_stripe_decrease_withdrawals WHERE quote_id=$1`, quoteID).Scan(&existing)
	if err == nil {
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if time.Now().Add(30*time.Second).Unix() >= q.Preview.EffectiveAt {
		return "", alertsstripe.ErrSubscription
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT d.state='scheduled' AND s.snapshot IS NOT NULL AND (b.binding->>'PaidSlots')::bigint=$3 FROM alerts_stripe_decreases d JOIN alerts_stripe_decrease_schedules s ON s.quote_id=d.quote_id JOIN alerts_stripe_checkouts c ON c.id=d.checkout_id JOIN alerts_stripe_current_subscriptions b ON b.checkout_id=c.id WHERE d.quote_id=$1 AND c.person_id=$2 AND c.closed_at IS NULL FOR UPDATE OF c,d`, quoteID, person, q.Preview.PreviousSlots).Scan(&allowed)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", alertsstripe.ErrSubscription
	}
	if _, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_decrease_withdrawals(quote_id) VALUES($1)`, quoteID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_checkouts SET next_reconcile_at=now() WHERE id=$1`, q.CheckoutID); err != nil {
		return "", err
	}
	return "pending", tx.Commit(ctx)
}

func claimAlertsStripeDecreaseWithdrawal(ctx context.Context, app *config.AppContext, person, quoteID string) (*alertsDecreaseWithdrawal, error) {
	q, err := AlertsStripeDecreaseQuoteForPerson(ctx, app, person, quoteID)
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
	out := &alertsDecreaseWithdrawal{Quote: *q}
	var raw []byte
	var lease time.Time
	var decreaseState string
	err = tx.QueryRow(ctx, `SELECT w.id::text,w.state,w.created_at,w.attempts,w.lease_until,s.schedule_id,b.binding,d.state FROM alerts_stripe_decrease_withdrawals w JOIN alerts_stripe_decreases d ON d.quote_id=w.quote_id JOIN alerts_stripe_decrease_schedules s ON s.quote_id=d.quote_id JOIN alerts_stripe_checkouts c ON c.id=d.checkout_id JOIN alerts_stripe_current_subscriptions b ON b.checkout_id=c.id WHERE w.quote_id=$1 AND c.person_id=$2 FOR UPDATE OF c,d,w`, quoteID, person).Scan(&out.ID, &out.State, &out.CreatedAt, &out.Attempt, &lease, &out.ScheduleID, &raw, &decreaseState)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &out.Binding) != nil || out.Binding.PersonID != person || out.Binding.IntentID != q.CheckoutID {
		return nil, alertsstripe.ErrSubscription
	}
	if out.State == "released" {
		return out, tx.Commit(ctx)
	}
	if out.State != "pending" || decreaseState != "scheduled" || lease.After(time.Now()) {
		return nil, alertsstripe.ErrReconcile
	}
	err = tx.QueryRow(ctx, `UPDATE alerts_stripe_decrease_withdrawals SET attempts=attempts+1,lease_until=now()+interval '2 minutes' WHERE id=$1 RETURNING attempts`, out.ID).Scan(&out.Attempt)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func recordAlertsStripeDecreaseWithdrawal(ctx context.Context, app *config.AppContext, in *alertsDecreaseWithdrawal, state alertsstripe.SubscriptionState) error {
	if state.State != "active" || state.ProviderStatus != "active" || state.CancelAtPeriodEnd || state.CancelAt != 0 || state.CollectionPaused || state.PeriodStart != in.Quote.Preview.PeriodStart || state.PeriodEnd != in.Quote.Preview.EffectiveAt {
		return alertsstripe.ErrSubscription
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+in.Binding.PersonID); err != nil {
		return err
	}
	updated, err := tx.Exec(ctx, `UPDATE alerts_stripe_decrease_withdrawals w SET state='released',snapshot=$3 WHERE w.id=$1 AND w.attempts=$2 AND w.state='pending' AND EXISTS(SELECT 1 FROM alerts_stripe_decreases d WHERE d.quote_id=w.quote_id AND d.state='scheduled')`, in.ID, in.Attempt, raw)
	if err != nil {
		return err
	}
	if updated.RowsAffected() != 1 {
		return alertsstripe.ErrReconcile
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='cancelled',updated_at=now() WHERE quote_id=$1`, in.Quote.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=revision+1,snapshot=$2,observed_at=now() WHERE checkout_id=$1`, in.Quote.CheckoutID, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ProcessAlertsStripeDecreaseWithdrawal(ctx context.Context, app *config.AppContext, person, quoteID string, client alertsstripe.Client) error {
	in, err := claimAlertsStripeDecreaseWithdrawal(ctx, app, person, quoteID)
	if err != nil {
		return err
	}
	if in.State == "released" {
		return nil
	}
	state, err := client.WithdrawDecreaseSchedule(ctx, in.Binding, quoteID, in.ID, in.ScheduleID, in.Quote.Preview, in.CreatedAt, time.Now().UTC())
	if err != nil {
		if errors.Is(err, alertsstripe.ErrSubscription) || errors.Is(err, alertsstripe.ErrReconcile) {
			_, saveErr := app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_withdrawals SET state='review' WHERE id=$1 AND attempts=$2 AND state='pending'`, in.ID, in.Attempt)
			if saveErr != nil {
				return saveErr
			}
		}
		return err
	}
	return recordAlertsStripeDecreaseWithdrawal(ctx, app, in, state)
}
