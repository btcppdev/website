package getters

import (
	"context"
	"errors"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"github.com/jackc/pgx/v5"
)

// ReconcileDueServiceStripeCheckouts recovers known sessions and paid invoices.
// The schedule is also a lease: advance one row before provider I/O, with a
// one-minute attempt deadline shorter than the minimum fifteen-minute lease.
func ReconcileDueServiceStripeCheckouts(ctx context.Context, app *config.AppContext, checkout servicestripe.CheckoutClient, invoices servicestripe.InvoiceClient, subscriptions servicestripe.SubscriptionClient) (int, error) {
	return reconcileDueServiceStripeCheckouts(ctx, app, func(ctx context.Context, person, id, product string) error {
		handled, err := expireStoppedServiceStripeCheckout(ctx, app, person, id, product, checkout)
		if err != nil || handled {
			return err
		}
		return RecoverServiceStripePayments(ctx, app, person, id, product, checkout, invoices, subscriptions)
	})
}

func reconcileDueServiceStripeCheckouts(ctx context.Context, app *config.AppContext, recover func(context.Context, string, string, string) error) (int, error) {
	var firstErr error
	for completed := 0; completed < 20; completed++ {
		if err := ctx.Err(); err != nil {
			return completed, err
		}
		var person, id, product string
		err := app.DB.QueryRow(ctx, `WITH due AS (
 SELECT c.id FROM service_stripe_checkouts c
 WHERE c.product_family='insider' AND c.person_id IS NOT NULL AND (c.stripe_session_id IS NOT NULL OR EXISTS(SELECT 1 FROM service_sales_closures x WHERE x.service=c.product_family))
 AND c.closed_at IS NULL AND c.next_reconcile_at<=now()
 ORDER BY c.next_reconcile_at,c.id LIMIT 1 FOR UPDATE OF c SKIP LOCKED
) UPDATE service_stripe_checkouts c SET next_reconcile_at=now()+CASE
 WHEN EXISTS(SELECT 1 FROM service_stripe_cancellations x WHERE x.checkout_id=c.id AND x.state='pending') THEN interval '15 minutes'
 WHEN EXISTS(SELECT 1 FROM service_stripe_invoices i WHERE i.checkout_id=c.id) THEN interval '1 hour'
 WHEN c.created_at<now()-interval '7 days' THEN interval '1 day' ELSE interval '15 minutes' END
 FROM due WHERE c.id=due.id RETURNING c.person_id::text,c.id::text,c.product_code`).Scan(&person, &id, &product)
		if errors.Is(err, pgx.ErrNoRows) {
			return completed, firstErr
		}
		if err != nil {
			return completed, err
		}
		attempt, cancel := context.WithTimeout(ctx, time.Minute)
		err = recover(attempt, person, id, product)
		cancel()
		if err != nil && !errors.Is(err, servicestripe.ErrReconcile) && firstErr == nil {
			firstErr = err
		}
	}
	return 20, firstErr
}
