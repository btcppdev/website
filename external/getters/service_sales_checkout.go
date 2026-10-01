package getters

import (
	"context"
	"errors"

	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
)

// Expiration uses the existing checkout row as durable retry intent. Completed
// sessions continue through settlement, and bound subscriptions are never expired.
func expireStoppedServiceStripeCheckout(ctx context.Context, app *config.AppContext, person, id, product string, client servicestripe.CheckoutClient) (bool, error) {
	err := checkServiceSales(ctx, app.DB, product)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, ErrServiceSalesClosed) {
		return false, err
	}
	var bound bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_stripe_subscriptions WHERE checkout_id=$1)`, id).Scan(&bound); err != nil {
		return false, err
	}
	if bound {
		return false, nil
	}
	stored, err := ServiceStripeCheckoutForPerson(app, person, id, product)
	if err != nil {
		return true, err
	}
	if stored.Closed {
		return true, nil
	}
	if stored.Reference == "" {
		reference, err := client.FindExistingCheckout(ctx, stored.Intent)
		if err != nil {
			return true, err
		}
		result, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET stripe_session_id=$3 WHERE person_id=$1 AND id=$2 AND closed_at IS NULL AND (stripe_session_id IS NULL OR stripe_session_id=$3)`, person, id, reference)
		if err != nil {
			return true, err
		}
		if result.RowsAffected() != 1 {
			return true, ErrServiceStripeCheckoutConflict
		}
		stored.Reference = reference
	}
	if err := client.ExpireConflictingCheckout(ctx, stored.Intent, stored.Reference); err != nil {
		if errors.Is(err, servicestripe.ErrReconcile) {
			return false, nil
		}
		return true, err
	}
	return true, CloseExpiredServiceStripeCheckout(ctx, app, person, id, product, stored.Reference, client)
}
