package getters

import (
	"context"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
)

// CancelAlertsStripeCheckout releases only a never-submitted reservation or a
// provider-verified expired, unpaid session. Ambiguous payments remain locked.
func CancelAlertsStripeCheckout(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	stored, err := AlertsStripeCheckoutForPerson(app, person, id)
	if err != nil || stored.Closed {
		return err
	}
	result, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts c SET closed_at=now()
 WHERE c.id=$1 AND c.person_id=$2 AND c.product_code='alerts.repository-slots' AND c.closed_at IS NULL
 AND c.provider_started_at IS NULL AND c.stripe_session_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM service_stripe_subscriptions s WHERE s.checkout_id=c.id)`, id, person)
	if err != nil || result.RowsAffected() == 1 {
		return err
	}
	stored, err = recoverAlertsStripeCheckoutReference(ctx, app, person, id, client)
	if err != nil || stored.Closed {
		return err
	}
	if err := client.ExpireConflictingCheckout(ctx, stored.Intent, stored.Reference); err != nil {
		return err
	}
	return CloseExpiredAlertsStripeCheckout(ctx, app, person, id, stored.Reference, client)
}
