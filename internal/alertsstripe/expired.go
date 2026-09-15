package alertsstripe

import (
	"context"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
)

// ExpiredUnpaid verifies terminal provider state, never inferring it from a
// timeout, local age, browser cancellation or a webhook payload alone.
func (c Client) ExpiredUnpaid(ctx context.Context, in Intent, reference string) error {
	if c.Retrieve == nil || !strings.HasPrefix(reference, "cs_") || len(reference) > 255 {
		return ErrCheckout
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	params := &stripe.CheckoutSessionParams{}
	params.Context = ctx
	s, err := c.Retrieve(reference, params)
	if err != nil {
		return err
	}
	if s == nil || s.ID != reference || s.Livemode != c.Live || s.Mode != "subscription" || s.ClientReferenceID != in.ID || s.Metadata["account_id"] != in.PersonID || s.Metadata["checkout_intent"] != in.ID || s.Metadata["product"] != "alerts.repository-slots" {
		return ErrCheckout
	}
	if s.Status != "expired" || s.PaymentStatus != "unpaid" || s.Subscription != nil || s.Invoice != nil || s.PaymentIntent != nil {
		return ErrReconcile
	}
	return nil
}
