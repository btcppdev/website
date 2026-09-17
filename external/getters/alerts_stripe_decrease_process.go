package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// ProcessAlertsStripeDecrease only operates on durable customer confirmation.
// Attachment and phase updates each commit their own lease before provider I/O,
// so a crash between the two steps is recoverable without another confirmation.
func ProcessAlertsStripeDecrease(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	if _, err := AlertsStripeDecreaseQuoteForPerson(ctx, app, person, id); err != nil {
		return err
	}
	var state string
	if err := app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_decreases WHERE quote_id=$1`, id).Scan(&state); err != nil {
		return err
	}
	var withdrawal string
	err := app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_decrease_withdrawals WHERE quote_id=$1`, id).Scan(&withdrawal)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if withdrawal == "pending" {
		return ProcessAlertsStripeDecreaseWithdrawal(ctx, app, person, id, client)
	}
	if withdrawal == "review" {
		return alertsstripe.ErrReconcile
	}
	switch state {
	case "pending":
		if _, err := AttachAlertsStripeDecrease(ctx, app, person, id, client); err != nil {
			return err
		}
		return ScheduleAlertsStripeDecrease(ctx, app, person, id, client)
	case "scheduled":
		return ReconcileAlertsStripeDecrease(ctx, app, person, id, client)
	default:
		return nil
	}
}
