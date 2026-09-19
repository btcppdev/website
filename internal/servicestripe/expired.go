package servicestripe

import (
	"context"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
)

// ExpiredUnpaid verifies terminal provider state, never inferring it from a
// timeout, local age, browser cancellation or a webhook payload alone.
func (c CheckoutClient) ExpiredUnpaid(ctx context.Context, in CheckoutIntent, reference string) error {
	s, err := c.lookupCheckout(ctx, in, reference)
	if err != nil {
		return err
	}
	if s.Status != "expired" || s.PaymentStatus != "unpaid" || s.Subscription != nil || s.Invoice != nil || s.PaymentIntent != nil {
		return ErrReconcile
	}
	return nil
}

type CheckoutState struct{ Status, InvoiceID string }

// CheckoutStatus locates recovery work; it is never evidence for an access grant.
func (c CheckoutClient) CheckoutStatus(ctx context.Context, in CheckoutIntent, reference string) (CheckoutState, error) {
	s, err := c.lookupCheckout(ctx, in, reference)
	if err != nil {
		return CheckoutState{}, err
	}
	out := CheckoutState{Status: string(s.Status)}
	if s.Invoice != nil {
		out.InvoiceID = s.Invoice.ID
	}
	if out.Status != "open" && out.Status != "complete" && out.Status != "expired" {
		return CheckoutState{}, ErrCheckout
	}
	return out, nil
}

func (c CheckoutClient) lookupCheckout(ctx context.Context, in CheckoutIntent, reference string) (*stripe.CheckoutSession, error) {
	if _, err := CheckoutParams(ctx, in, in.CreatedAt); err != nil {
		return nil, err
	}
	if c.Retrieve == nil || !strings.HasPrefix(reference, "cs_") || len(reference) > 255 {
		return nil, ErrCheckout
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	params := &stripe.CheckoutSessionParams{}
	params.Context = ctx
	s, err := c.Retrieve(reference, params)
	if err != nil {
		return nil, err
	}
	if s == nil || s.ID != reference || s.Livemode != c.Live || s.Mode != "subscription" || s.ClientReferenceID != in.ID || s.Metadata["account_id"] != in.PersonID || s.Metadata["checkout_intent"] != in.ID || s.Metadata["product"] != in.ProductCode {
		return nil, ErrCheckout
	}
	return s, nil
}
