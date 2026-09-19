package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
)

// CloseExpiredServiceStripeCheckout frees only a verified unpaid, expired
// reservation. It preserves the session reference even after a lost creation
// response and never removes financial evidence or subscription bindings.
func CloseExpiredServiceStripeCheckout(ctx context.Context, app *config.AppContext, person, id, product, reference string, client servicestripe.CheckoutClient) error {
	stored, err := ServiceStripeCheckoutForPerson(app, person, id, product)
	if err != nil {
		return err
	}
	if reference == "" {
		reference = stored.Reference
	}
	if reference == "" || (stored.Reference != "" && stored.Reference != reference) {
		return ErrServiceStripeCheckoutConflict
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
	if err = tx.QueryRow(ctx, `SELECT coalesce(stripe_session_id,'') FROM service_stripe_checkouts WHERE id=$1 AND person_id=$2 FOR UPDATE`, id, person).Scan(&current); err != nil {
		return err
	}
	if current != "" && current != reference {
		return ErrServiceStripeCheckoutConflict
	}
	var bound bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_stripe_subscriptions WHERE checkout_id=$1)`, id).Scan(&bound); err != nil {
		return err
	}
	if bound {
		return ErrServiceStripeCheckoutConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE service_stripe_checkouts SET stripe_session_id=coalesce(stripe_session_id,$3),closed_at=coalesce(closed_at,now()) WHERE id=$1 AND person_id=$2`, id, person, reference); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
