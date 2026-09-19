package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
)

// ResolveServiceStripeSubscription binds a product-scoped saved checkout to
// authenticated Stripe evidence. Binding grants no access; invoices do that.
func ResolveServiceStripeSubscription(ctx context.Context, app *config.AppContext, person, id, product string, client servicestripe.CheckoutClient, recoveredReference ...string) (*servicestripe.Binding, error) {
	stored, err := ServiceStripeCheckoutForPerson(app, person, id, product)
	if err != nil {
		return nil, err
	}
	if len(recoveredReference) > 1 {
		return nil, servicestripe.ErrCheckout
	}
	if len(recoveredReference) == 1 {
		if stored.Reference != "" && stored.Reference != recoveredReference[0] {
			return nil, servicestripe.ErrCheckout
		}
		stored.Reference = recoveredReference[0]
	}
	binding, err := client.CompletedBinding(ctx, stored.Intent, stored.Reference)
	if err != nil {
		return nil, err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return nil, err
	}
	result, err := tx.Exec(ctx, `UPDATE service_stripe_checkouts SET stripe_session_id=$3 WHERE id=$1 AND person_id=$2 AND closed_at IS NULL AND (stripe_session_id IS NULL OR stripe_session_id=$3)`, id, person, stored.Reference)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, servicestripe.ErrCheckout
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3) ON CONFLICT(checkout_id) DO NOTHING`, id, binding.SubscriptionID, raw)
	if err != nil {
		return nil, err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT subscription_id=$2 AND binding=$3::jsonb FROM service_stripe_subscriptions WHERE checkout_id=$1`, id, binding.SubscriptionID, raw).Scan(&same)
	if err != nil {
		return nil, err
	}
	if !same {
		return nil, servicestripe.ErrInvoice
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return binding, nil
}
