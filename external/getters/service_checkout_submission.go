package getters

import (
	"context"

	"btcpp-web/internal/config"
)

// Commit before provider I/O. Even a timeout or process crash then leaves durable
// evidence that Stripe may have received a request. Sales stop serializes with
// this claim; a closed reservation can never be submitted later.
func markServiceStripeCheckoutStarted(ctx context.Context, app *config.AppContext, person, id, product string) error {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireServiceSales(ctx, tx, product); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE service_stripe_checkouts SET provider_started_at=coalesce(provider_started_at,now()) WHERE id=$1 AND person_id=$2 AND product_code=$3 AND closed_at IS NULL`, id, person, product)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrServiceStripeCheckoutConflict
	}
	return tx.Commit(ctx)
}

// No provider evidence is needed only when the reservation explicitly records
// that submission never began. Imported/legacy rows default to possibly submitted.
// The row update serializes with submission claims and reference binding.
func closeUnsubmittedStoppedServiceCheckout(ctx context.Context, app *config.AppContext, person, id, product string) (bool, error) {
	result, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts c SET closed_at=now()
 WHERE c.id=$1 AND c.person_id=$2 AND c.product_code=$3 AND c.closed_at IS NULL
 AND c.provider_started_at IS NULL AND c.stripe_session_id IS NULL
 AND EXISTS(SELECT 1 FROM service_sales_closures x WHERE x.service=c.product_family)
 AND NOT EXISTS(SELECT 1 FROM service_stripe_subscriptions s WHERE s.checkout_id=c.id)`, id, person, product)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}
