package getters

import (
	"context"
	"errors"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

// ReconcileDueAlertsStripeCheckouts recovers known sessions and paid invoices.
// The schedule is also a lease: advance one row before provider I/O, with a
// one-minute attempt deadline shorter than the minimum two-minute lease.
func ReconcileDueAlertsStripeCheckouts(ctx context.Context, app *config.AppContext, client alertsstripe.Client) (int, error) {
	return reconcileDueAlertsStripeCheckouts(ctx, app, func(ctx context.Context, person, id string) error {
		if _, err := recoverAlertsStripeCheckoutReference(ctx, app, person, id, client); err != nil {
			return err
		}
		handled, err := expireConflictingAlertsCheckout(ctx, app, person, id, client)
		if err != nil || handled {
			return err
		}
		return RecoverAlertsStripePayments(ctx, app, person, id, client)
	})
}

func reconcileDueAlertsStripeCheckouts(ctx context.Context, app *config.AppContext, recover func(context.Context, string, string) error) (int, error) {
	var firstErr error
	for completed := 0; completed < 20; completed++ {
		if err := ctx.Err(); err != nil {
			return completed, err
		}
		var person, id string
		err := app.DB.QueryRow(ctx, `WITH due AS (
 SELECT c.id FROM alerts_stripe_checkouts c
 WHERE c.person_id IS NOT NULL
 AND c.closed_at IS NULL AND c.next_reconcile_at<=now()
 ORDER BY c.next_reconcile_at,c.id LIMIT 1 FOR UPDATE OF c SKIP LOCKED
) UPDATE alerts_stripe_checkouts c SET next_reconcile_at=now()+CASE
 WHEN EXISTS(SELECT 1 FROM alerts_stripe_cancellations x WHERE x.checkout_id=c.id AND x.state='pending') THEN interval '15 minutes'
 WHEN EXISTS(SELECT 1 FROM alerts_stripe_increases x WHERE x.checkout_id=c.id AND x.state='pending') THEN interval '2 minutes'
 WHEN EXISTS(SELECT 1 FROM alerts_stripe_decreases x WHERE x.checkout_id=c.id AND x.state='pending') THEN interval '2 minutes'
 WHEN EXISTS(SELECT 1 FROM alerts_stripe_decrease_withdrawals w JOIN alerts_stripe_decreases d ON d.quote_id=w.quote_id WHERE d.checkout_id=c.id AND w.state='pending') THEN interval '2 minutes'
 WHEN EXISTS(SELECT 1 FROM alerts_stripe_invoices i WHERE i.checkout_id=c.id) THEN interval '1 hour'
 WHEN c.created_at<now()-interval '7 days' THEN interval '1 day' ELSE interval '15 minutes' END
 FROM due WHERE c.id=due.id RETURNING c.person_id::text,c.id::text`).Scan(&person, &id)
		if errors.Is(err, pgx.ErrNoRows) {
			return completed, firstErr
		}
		if err != nil {
			return completed, err
		}
		attempt, cancel := context.WithTimeout(ctx, time.Minute)
		err = recover(attempt, person, id)
		cancel()
		if err != nil && !errors.Is(err, alertsstripe.ErrReconcile) && firstErr == nil {
			firstErr = err
		}
	}
	return 20, firstErr
}

// The existing checkout row/reference and reconciliation schedule are the durable
// retry intent. Never close locally before confirmed provider expiration, and
// never run this path for an already-bound recurring subscription.
func expireConflictingAlertsCheckout(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (bool, error) {
	var bound bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_subscriptions WHERE checkout_id=$1)`, id).Scan(&bound); err != nil {
		return false, err
	}
	if bound {
		return false, nil
	}
	var conflict bool
	if err := app.DB.QueryRow(ctx, alertsStripePurchaseConflictSQL, person).Scan(&conflict); err != nil {
		return false, err
	}
	if !conflict {
		return false, nil
	}
	stored, err := AlertsStripeCheckoutForPerson(app, person, id)
	if err != nil {
		return true, err
	}
	if stored.Closed {
		return true, nil
	}
	if err = client.ExpireConflictingCheckout(ctx, stored.Intent, stored.Reference); err != nil {
		if errors.Is(err, alertsstripe.ErrReconcile) {
			return true, ErrAlertsStripeCheckoutConflict
		}
		return true, err
	}
	return true, CloseExpiredAlertsStripeCheckout(ctx, app, person, id, stored.Reference, client)
}
