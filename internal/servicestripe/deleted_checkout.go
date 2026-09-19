package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// ResolveDeletedCheckout expires a verified open checkout or returns a completed
// checkout's binding for cancellation. Nil binding and nil error means confirmed
// expiration. It never creates a new checkout or grants access.
func (c CheckoutClient) ResolveDeletedCheckout(ctx context.Context, in CheckoutIntent, reference string) (*Binding, error) {
	if _, err := CheckoutParams(ctx, in, in.CreatedAt); err != nil {
		return nil, err
	}
	if c.Retrieve == nil || !strings.HasPrefix(reference, "cs_") || len(reference) > 255 {
		return nil, ErrCheckout
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	params := &stripe.CheckoutSessionParams{}
	params.Context = ctx
	s, err := c.Retrieve(reference, params)
	if err != nil {
		return nil, err
	}
	valid := func(s *stripe.CheckoutSession) bool {
		return s != nil && s.ID == reference && s.Mode == "subscription" && s.Livemode == c.Live && s.ClientReferenceID == in.ID && s.Metadata["account_id"] == in.PersonID && s.Metadata["checkout_intent"] == in.ID && s.Metadata["product"] == in.ProductCode
	}
	if !valid(s) {
		return nil, ErrCheckout
	}
	switch s.Status {
	case "complete":
		return c.CompletedBinding(ctx, in, reference)
	case "expired":
		if s.Subscription != nil {
			return nil, ErrCheckout
		}
		return nil, nil
	case "open":
		if c.Expire == nil || s.Subscription != nil {
			return nil, ErrCheckout
		}
		p := &stripe.CheckoutSessionExpireParams{}
		p.Context = ctx
		p.SetIdempotencyKey("deleted-account-expire:" + in.ID)
		expired, err := c.Expire(reference, p)
		if err != nil {
			return nil, err
		}
		if !valid(expired) || expired.Status != "expired" || expired.Subscription != nil {
			return nil, ErrCheckout
		}
		return nil, nil
	default:
		return nil, ErrCheckout
	}
}
