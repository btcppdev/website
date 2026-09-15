package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
)

// RecoverAlertsStripeCheckout only reads provider state. It reuses the same
// binding/invoice validators and atomic persistence paths as signed callbacks.
// It cannot create a session, charge, subscription or replacement checkout.
func RecoverAlertsStripeCheckout(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	stored, err := AlertsStripeCheckoutForPerson(app, person, id)
	if err != nil {
		return err
	}
	if stored.Closed {
		return nil
	}
	state, err := client.CheckoutStatus(ctx, stored.Intent, stored.Reference)
	if err != nil {
		return err
	}
	switch state.Status {
	case "expired":
		return CloseExpiredAlertsStripeCheckout(ctx, app, person, id, stored.Reference, client)
	case "complete":
		if _, err = ResolveAlertsStripeSubscription(ctx, app, person, id, client); err != nil {
			return err
		}
		if state.InvoiceID == "" {
			return alertsstripe.ErrReconcile
		}
		_, err = ReconcileAlertsStripeInvoice(ctx, app, person, id, state.InvoiceID, client)
		return err
	default:
		return nil
	}
}
