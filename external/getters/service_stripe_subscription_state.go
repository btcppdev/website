package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
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
	if err = saveServiceStripeSubscriptionState(ctx, tx, id, state); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func saveServiceStripeSubscriptionState(ctx context.Context, tx pgx.Tx, id string, state servicestripe.SubscriptionState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}

	var revision int64
	var observed time.Time
	err = tx.QueryRow(ctx, `INSERT INTO service_stripe_subscription_state(checkout_id,revision,snapshot) VALUES($1,1,$2)
 ON CONFLICT(checkout_id) DO UPDATE SET revision=service_stripe_subscription_state.revision+CASE WHEN service_stripe_subscription_state.snapshot<>excluded.snapshot THEN 1 ELSE 0 END,snapshot=excluded.snapshot,observed_at=now() RETURNING revision,observed_at`, id, raw).Scan(&revision, &observed)
	if err != nil {
		return err
	}
	var person, email, product string
	if err = tx.QueryRow(ctx, `SELECT person_id::text,intent->>'Email',product_code FROM service_stripe_checkouts WHERE id=$1 AND product_family='insider' AND person_id IS NOT NULL`, id).Scan(&person, &email, &product); err != nil {
		return err
	}
	event := ServiceLifecycleEvent{SchemaVersion: 1, EventID: fmt.Sprintf("subscription:%s:%d", id, revision), EventKind: "subscription_state", CheckoutID: id, Revision: revision, PersonID: person, BuyerEmail: email, ProductCode: product, ObservedAt: observed.UTC(), Subscription: state}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_lifecycle_jobs(checkout_id,revision,event) VALUES($1,$2,$3) ON CONFLICT(checkout_id,revision) DO NOTHING`, id, revision, payload)
	return err
}

// ServiceLifecycleEvent reports verified billing state, never an access grant.
// Consumers fence revisions per checkout and deduplicate confirmations by event ID.
type ServiceLifecycleEvent struct {
	SchemaVersion int                             `json:"schema_version"`
	EventID       string                          `json:"event_id"`
	EventKind     string                          `json:"event_kind"`
	CheckoutID    string                          `json:"checkout_id"`
	Revision      int64                           `json:"revision"`
	PersonID      string                          `json:"person_id"`
	BuyerEmail    string                          `json:"buyer_email"`
	ProductCode   string                          `json:"product_code"`
	ObservedAt    time.Time                       `json:"observed_at"`
	Subscription  servicestripe.SubscriptionState `json:"subscription"`
}
