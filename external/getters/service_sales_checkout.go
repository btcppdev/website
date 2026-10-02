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
	if closed, err := closeUnsubmittedStoppedServiceCheckout(ctx, app, person, id, product); err != nil || closed {
		return closed, err
	}
	var bound bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_stripe_subscriptions WHERE checkout_id=$1)`, id).Scan(&bound); err != nil {
		return false, err
	}
	if bound {
		return false, nil
	}
	stored, err := recoverServiceStripeCheckoutReference(ctx, app, person, id, product, client)
	if err != nil {
		return true, err
	}
	if stored.Closed {
		return true, nil
	}
	if err := client.ExpireConflictingCheckout(ctx, stored.Intent, stored.Reference); err != nil {
		if errors.Is(err, servicestripe.ErrReconcile) {
			return false, nil
		}
		return true, err
	}
	return true, CloseExpiredServiceStripeCheckout(ctx, app, person, id, product, stored.Reference, client)
}
