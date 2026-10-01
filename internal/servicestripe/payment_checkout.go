package servicestripe

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

// PaymentCheckout identifies a one-off session from immutable local accounting.
type PaymentCheckout struct {
	AttemptID  string `json:"attempt_id"`
	OrderID    string `json:"order_id"`
	Reference  string `json:"reference"`
	TotalCents int64  `json:"total_cents"`
}

type PaymentCheckoutExpiration struct {
	PaymentCheckout
	VerifiedAt time.Time `json:"verified_at"`
}

// ExpirePaymentCheckout never cancels a payment intent or discards a completed
// payment. Ambiguous or payment-bearing sessions require settlement/review.
func (c CheckoutClient) ExpirePaymentCheckout(ctx context.Context, in PaymentCheckout) (*PaymentCheckoutExpiration, error) {
	for _, raw := range []string{in.AttemptID, in.OrderID} {
		if id, err := uuid.Parse(raw); err != nil || id.String() != raw {
			return nil, ErrCheckout
		}
	}
	if !strings.HasPrefix(in.Reference, "cs_") || len(in.Reference) > 255 || in.TotalCents <= 0 || c.Retrieve == nil {
		return nil, ErrCheckout
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	params := &stripe.CheckoutSessionParams{}
	params.Context = ctx
	s, err := c.Retrieve(in.Reference, params)
	if err != nil {
		return nil, err
	}
	valid := func(s *stripe.CheckoutSession) bool {
		return s != nil && s.ID == in.Reference && s.Mode == "payment" && s.Livemode == c.Live && s.Metadata["checkout-kind"] == "service" && s.Metadata["service-attempt-id"] == in.AttemptID && s.Metadata["service-order-id"] == in.OrderID && s.Currency == "usd" && s.AmountTotal == in.TotalCents && s.Subscription == nil
	}
	if !valid(s) {
		return nil, ErrCheckout
	}
	if s.Status == "complete" || s.PaymentStatus == "paid" {
		return nil, ErrReconcile
	}
	if s.PaymentStatus != "unpaid" || s.PaymentIntent != nil || s.Invoice != nil {
		return nil, ErrReconcile
	}
	if s.Status == "open" {
		if c.Expire == nil {
			return nil, ErrCheckout
		}
		p := &stripe.CheckoutSessionExpireParams{}
		p.Context = ctx
		p.SetIdempotencyKey("service-closure-expire:" + in.AttemptID)
		s, err = c.Expire(in.Reference, p)
		if err != nil {
			return nil, err
		}
	}
	if !valid(s) || s.Status != "expired" || s.PaymentStatus != "unpaid" || s.PaymentIntent != nil || s.Invoice != nil {
		return nil, ErrReconcile
	}
	return &PaymentCheckoutExpiration{PaymentCheckout: in, VerifiedAt: time.Now().UTC()}, nil
}
