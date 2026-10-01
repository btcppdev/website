package servicestripe

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestExpirePaymentCheckout(t *testing.T) {
	for _, mode := range []string{"open", "expired", "paid", "pending-payment", "wrong-owner", "wrong-amount", "wrong-mode", "wrong-live", "lost-response"} {
		t.Run(mode, func(t *testing.T) {
			in := PaymentCheckout{AttemptID: uuid.NewString(), OrderID: uuid.NewString(), Reference: "cs_fixture", TotalCents: 200}
			s := &stripe.CheckoutSession{ID: in.Reference, Mode: "payment", Status: "open", PaymentStatus: "unpaid", Currency: "usd", AmountTotal: 200, Metadata: map[string]string{"checkout-kind": "service", "service-attempt-id": in.AttemptID, "service-order-id": in.OrderID}}
			switch mode {
			case "expired":
				s.Status = "expired"
			case "paid":
				s.Status = "complete"
				s.PaymentStatus = "paid"
			case "pending-payment":
				s.PaymentIntent = &stripe.PaymentIntent{ID: "pi_pending"}
			case "wrong-owner":
				s.Metadata["service-order-id"] = uuid.NewString()
			case "wrong-amount":
				s.AmountTotal++
			case "wrong-mode":
				s.Mode = "subscription"
			case "wrong-live":
				s.Livemode = true
			}
			calls := 0
			c := CheckoutClient{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return s, nil }, Expire: func(ref string, p *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
				calls++
				if ref != in.Reference || p.IdempotencyKey == nil || *p.IdempotencyKey != "service-closure-expire:"+in.AttemptID {
					t.Fatal("unbound mutation")
				}
				s.Status = "expired"
				if mode == "lost-response" {
					return nil, errors.New("lost response")
				}
				return s, nil
			}}
			got, err := c.ExpirePaymentCheckout(context.Background(), in)
			if mode == "open" || mode == "expired" {
				if err != nil || got == nil || got.PaymentCheckout != in || got.VerifiedAt.IsZero() {
					t.Fatal(got, err)
				}
			} else if err == nil || got != nil {
				t.Fatal("unverified expiration", got, err)
			}
			expected := 0
			if mode == "open" || mode == "lost-response" {
				expected = 1
			}
			if calls != expected {
				t.Fatal("unexpected mutation", calls)
			}
			if mode == "lost-response" {
				got, err = c.ExpirePaymentCheckout(context.Background(), in)
				if err != nil || got == nil || calls != 1 {
					t.Fatal("retry", got, err, calls)
				}
			}
		})
	}
}
