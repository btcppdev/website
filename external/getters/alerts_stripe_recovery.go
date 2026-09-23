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
	stored, err := recoverAlertsStripeCheckoutReference(ctx, app, person, id, client)
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

// Recover a lost creation response by listing authenticated provider state for
// the frozen intent. No match is retryable, not evidence that a checkout may be
// recreated or locally closed. A conflicting stored reference is never replaced.
func recoverAlertsStripeCheckoutReference(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (*AlertsStripeCheckout, error) {
	stored, err := AlertsStripeCheckoutForPerson(app, person, id)
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
	result, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$3 WHERE person_id=$1 AND id=$2 AND closed_at IS NULL AND (stripe_session_id IS NULL OR stripe_session_id=$3)`, person, id, reference)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, ErrAlertsStripeCheckoutConflict
	}
	stored.Reference = reference
	return stored, nil
}
