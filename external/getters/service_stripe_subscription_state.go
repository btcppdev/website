package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
)

// RefreshServiceStripeSubscription serializes the authenticated lookup with
// account billing operations. Old webhook bodies never replace current state.
// A status observation does not change paid periods or grant a grace period.
func RefreshServiceStripeSubscription(ctx context.Context, app *config.AppContext, person, id, product string, client servicestripe.SubscriptionClient) error {
	if _, err := ServiceStripeCheckoutForPerson(app, person, id, product); err != nil {
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
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT s.binding FROM service_stripe_subscriptions s JOIN service_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND c.product_code=$3 FOR UPDATE OF c`, id, person, product).Scan(&raw); err != nil {
		return err
	}
	var binding servicestripe.Binding
	if err = json.Unmarshal(raw, &binding); err != nil {
		return err
	}
	if binding.PersonID != person || binding.IntentID != id || binding.ProductCode != product {
		return servicestripe.ErrSubscription
	}
	state, err := client.CurrentSubscription(ctx, binding)
	if err != nil {
		return err
	}
	raw, err = json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_stripe_subscription_state(checkout_id,revision,snapshot) VALUES($1,1,$2)
 ON CONFLICT(checkout_id) DO UPDATE SET revision=service_stripe_subscription_state.revision+CASE WHEN service_stripe_subscription_state.snapshot<>excluded.snapshot THEN 1 ELSE 0 END,snapshot=excluded.snapshot,observed_at=now()`, id, raw)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
