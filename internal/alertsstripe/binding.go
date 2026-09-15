package alertsstripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// CompletedBinding resolves the authenticated Checkout Session and its expanded
// recurring price. It establishes ownership, not payment or access.
func (c Client) CompletedBinding(ctx context.Context, in Intent, reference string) (*Binding, error) {
	if !strings.HasPrefix(reference, "cs_") || c.Retrieve == nil {
		return nil, ErrCheckout
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := &stripe.CheckoutSessionParams{}
	p.Context = ctx
	p.AddExpand("line_items.data.price.product")
	s, err := c.Retrieve(reference, p)
	if err != nil {
		return nil, err
	}
	if s == nil || s.ID != reference || s.Mode != "subscription" || s.Status != "complete" || s.Livemode != c.Live || s.ClientReferenceID != in.ID || s.Metadata["checkout_intent"] != in.ID || s.Metadata["account_id"] != in.PersonID || s.Metadata["product"] != "alerts.repository-slots" || s.Subscription == nil || s.Customer == nil || s.LineItems == nil || s.LineItems.HasMore || len(s.LineItems.Data) != 1 {
		return nil, ErrCheckout
	}
	line := s.LineItems.Data[0]
	if line == nil || line.Quantity != in.PaidSlots || line.Price == nil || line.Price.Product == nil {
		return nil, ErrCheckout
	}
	price := line.Price
	b := &Binding{IntentID: in.ID, PersonID: in.PersonID, SubscriptionID: s.Subscription.ID, CustomerID: s.Customer.ID, PriceID: price.ID, ProductID: price.Product.ID, PaidSlots: in.PaidSlots, Live: c.Live}
	if !strings.HasPrefix(b.SubscriptionID, "sub_") || !strings.HasPrefix(b.CustomerID, "cus_") || !strings.HasPrefix(b.PriceID, "price_") || !strings.HasPrefix(b.ProductID, "prod_") || !validMonthlyPrice(price, *b) {
		return nil, ErrCheckout
	}
	return b, nil
}
