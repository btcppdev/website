package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"time"
)

type ServiceSubscriptionStatus struct {
	CheckoutID  string                          `json:"checkout_id"`
	ProductCode string                          `json:"product_code"`
	Revision    int64                           `json:"revision"`
	ObservedAt  time.Time                       `json:"observed_at"`
	State       servicestripe.SubscriptionState `json:"subscription"`
}

// ServiceStripeSubscriptionStatus is account/product scoped. Provider identifiers
// stay internal; this observation describes billing, not product access grants.
func ServiceStripeSubscriptionStatus(ctx context.Context, app *config.AppContext, person, id, product string) (*ServiceSubscriptionStatus, error) {
	if _, err := ServiceStripeCheckoutForPerson(app, person, id, product); err != nil {
		return nil, err
	}
	out := &ServiceSubscriptionStatus{CheckoutID: id, ProductCode: product}
	var raw []byte
	err := app.DB.QueryRow(ctx, `SELECT s.revision,s.observed_at,s.snapshot FROM service_stripe_subscription_state s JOIN service_stripe_checkouts c ON c.id=s.checkout_id WHERE c.person_id=$1 AND c.id=$2 AND c.product_code=$3`, person, id, product).Scan(&out.Revision, &out.ObservedAt, &raw)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.State); err != nil {
		return nil, err
	}
	// Invoice identifiers are provider details, not a management capability.
	out.State.LatestInvoice = ""
	return out, nil
}

// CurrentInsiderStripeCheckout resolves the reader's active recurring agreement
// without requiring Insider to maintain its own provider-object mapping.
func CurrentInsiderStripeCheckout(ctx context.Context, app *config.AppContext, person string) (id, product string, err error) {
	err = app.DB.QueryRow(ctx, `SELECT c.id::text,c.product_code FROM service_stripe_checkouts c JOIN service_stripe_subscriptions s ON s.checkout_id=c.id WHERE c.person_id=$1 AND c.product_family='insider' AND c.closed_at IS NULL`, person).Scan(&id, &product)
	return
}
