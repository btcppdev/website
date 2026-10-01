package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"errors"
)

// ProcessAlertsStripeIncrease resumes durable customer consent. It discovers
// existing payment evidence before considering a submission. Both browser POST
// and the background worker use the same fenced, idempotent operation.
func ProcessAlertsStripeIncrease(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	if _, err := AlertsStripeIncreaseQuoteForPerson(ctx, app, person, id); err != nil {
		return err
	}
	var state string
	if err := app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, id).Scan(&state); err != nil {
		return err
	}
	if state != "pending" {
		return nil
	}
	if handled, err := expireUnsubmittedStoppedAlertsIncrease(ctx, app, person, id); err != nil || handled {
		return err
	}
	recovered, err := RecoverAlertsStripeIncrease(ctx, app, person, id, client)
	if err != nil {
		return err
	}
	if recovered {
		return nil
	}
	// Recovery may have resolved a void invoice and expired the instruction.
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, id).Scan(&state); err != nil {
		return err
	}
	if state != "pending" {
		return nil
	}
	invoice, err := SubmitAlertsStripeIncrease(ctx, app, person, id, client)
	if err != nil {
		return err
	}
	// A synchronous successful payment can be fulfilled immediately. Open
	// invoices remain pending; their existing payment link handles authentication.
	_, err = ReconcileAlertsStripeIncreaseInvoice(ctx, app, person, id, invoice, client)
	if errors.Is(err, alertsstripe.ErrInvoice) {
		return nil
	}
	return err
}
