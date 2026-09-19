package alertsstripe

import (
	"btcpp-web/internal/servicestripe"
	"context"
)

// CompletedBinding establishes ownership from authenticated Stripe evidence;
// a completed checkout alone does not establish payment or grant access.
func (c Client) CompletedBinding(ctx context.Context, in Intent, reference string) (*Binding, error) {
	b, err := (servicestripe.CheckoutClient{Retrieve: c.Retrieve, Live: c.Live}).CompletedBinding(ctx, serviceCheckoutIntent(in), reference)
	if err != nil {
		return nil, err
	}
	return &Binding{IntentID: b.IntentID, PersonID: b.PersonID, SubscriptionID: b.SubscriptionID, CustomerID: b.CustomerID, PriceID: b.PriceID, ProductID: b.ProductID, PaidSlots: b.Quantity, Live: b.Live}, nil
}
