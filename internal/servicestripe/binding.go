package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// CompletedBinding resolves the authenticated Checkout Session and its expanded
// recurring price. It establishes ownership, not payment or access.
func (c CheckoutClient) CompletedBinding(ctx context.Context, in CheckoutIntent, reference string) (*Binding, error) {
	if _, err := CheckoutParams(ctx, in, in.CreatedAt); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(reference, "cs_") || len(reference) > 255 || c.Retrieve == nil {
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
	if s == nil || s.ID != reference || s.Mode != "subscription" || s.Status != "complete" || s.Livemode != c.Live || s.ClientReferenceID != in.ID || s.Metadata["checkout_intent"] != in.ID || s.Metadata["account_id"] != in.PersonID || s.Metadata["product"] != in.ProductCode || s.Subscription == nil || s.Customer == nil || s.LineItems == nil || s.LineItems.HasMore || len(s.LineItems.Data) != 1 {
		return nil, ErrCheckout
	}
	line := s.LineItems.Data[0]
	if line == nil || line.Quantity != in.Quantity || line.Price == nil || line.Price.Product == nil {
		return nil, ErrCheckout
	}
	price := line.Price
	b := &Binding{IntentID: in.ID, PersonID: in.PersonID, SubscriptionID: s.Subscription.ID, CustomerID: s.Customer.ID, PriceID: price.ID, ProductID: price.Product.ID, Quantity: in.Quantity, ProductCode: in.ProductCode, TaxCode: in.TaxCode, Live: c.Live}
	if !strings.HasPrefix(b.SubscriptionID, "sub_") || !strings.HasPrefix(b.CustomerID, "cus_") || !strings.HasPrefix(b.PriceID, "price_") || !strings.HasPrefix(b.ProductID, "prod_") || !ValidPrice(price, *b) {
		return nil, ErrCheckout
	}
	return b, nil
}
