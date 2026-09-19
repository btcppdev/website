package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
)

// RecoverServiceStripeCheckout only reads provider state. It never creates a
// replacement charge. Subscription renewal discovery is a separate lifecycle job.
func RecoverServiceStripeCheckout(ctx context.Context, app *config.AppContext, person, id, product string, checkout servicestripe.CheckoutClient, invoices servicestripe.InvoiceClient) error {
	stored, err := ServiceStripeCheckoutForPerson(app, person, id, product)
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
