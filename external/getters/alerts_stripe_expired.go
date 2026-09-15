package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
)

// CloseExpiredAlertsStripeCheckout frees only a verified unpaid, expired
// reservation. It preserves the session reference even after a lost creation
// response and never removes financial evidence or subscription bindings.
func CloseExpiredAlertsStripeCheckout(ctx context.Context, app *config.AppContext, person, id, reference string, client alertsstripe.Client) error {
	stored, err := AlertsStripeCheckoutForPerson(app, person, id)
	if err != nil {
		return err
	}
	if reference == "" {
		reference = stored.Reference
	}
	if reference == "" || (stored.Reference != "" && stored.Reference != reference) {
		return ErrAlertsStripeCheckoutConflict
	}
	if err = client.ExpiredUnpaid(ctx, stored.Intent, reference); err != nil {
		return err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT coalesce(stripe_session_id,'') FROM alerts_stripe_checkouts WHERE id=$1 AND person_id=$2 FOR UPDATE`, id, person).Scan(&current); err != nil {
		return err
	}
	if current != "" && current != reference {
		return ErrAlertsStripeCheckoutConflict
	}
	var bound bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_subscriptions WHERE checkout_id=$1)`, id).Scan(&bound); err != nil {
		return err
	}
	if bound {
		return ErrAlertsStripeCheckoutConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=coalesce(stripe_session_id,$3),closed_at=coalesce(closed_at,now()) WHERE id=$1 AND person_id=$2`, id, person, reference); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
