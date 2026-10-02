package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
)

// RecoverServiceStripeCheckout only reads provider state. It never creates a
// replacement charge. Subscription renewal discovery is a separate lifecycle job.
func RecoverServiceStripeCheckout(ctx context.Context, app *config.AppContext, person, id, product string, checkout servicestripe.CheckoutClient, invoices servicestripe.InvoiceClient) error {
	stored, err := recoverServiceStripeCheckoutReference(ctx, app, person, id, product, checkout)
	if err != nil {
		return err
	}
	if stored.Closed {
		return nil
	}
	state, err := checkout.CheckoutStatus(ctx, stored.Intent, stored.Reference)
	if err != nil {
		return err
	}
	switch state.Status {
	case "expired":
		return CloseExpiredServiceStripeCheckout(ctx, app, person, id, product, stored.Reference, checkout)
	case "complete":
		if _, err = ResolveServiceStripeSubscription(ctx, app, person, id, product, checkout); err != nil {
			return err
		}
		if state.InvoiceID == "" {
			return servicestripe.ErrReconcile
		}
		_, err = ReconcileServiceStripeInvoice(ctx, app, person, id, product, state.InvoiceID, invoices)
		return err
	default:
		return nil
	}
}

// Discover a missing response without creating another provider session. Only a
// unique complete match may bind; concurrent responses cannot be overwritten.
func recoverServiceStripeCheckoutReference(ctx context.Context, app *config.AppContext, person, id, product string, client servicestripe.CheckoutClient) (*ServiceStripeCheckout, error) {
	stored, err := ServiceStripeCheckoutForPerson(app, person, id, product)
	if err != nil {
		return nil, err
	}
	if stored.Closed || stored.Reference != "" {
		return stored, nil
	}
	reference, err := client.FindExistingCheckout(ctx, stored.Intent)
	if err != nil {
		return nil, err
	}
	result, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET stripe_session_id=$4 WHERE person_id=$1 AND id=$2 AND product_code=$3 AND closed_at IS NULL AND (stripe_session_id IS NULL OR stripe_session_id=$4)`, person, id, product, reference)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, ErrServiceStripeCheckoutConflict
	}
	stored.Reference = reference
	return stored, nil
}
