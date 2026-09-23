package servicestripe

import (
	"context"
	"errors"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestConflictingCheckoutExpiration(t *testing.T) {
	for _, scenario := range []string{"open", "lost response", "completed", "wrong owner", "paid open", "unknown payment", "invoice attached", "unconfirmed"} {
		t.Run(scenario, func(t *testing.T) {
			in := insiderIntent("insider.membership.monthly")
			s := &stripe.CheckoutSession{ID: "cs_conflict", Mode: "subscription", Status: "open", PaymentStatus: "unpaid", ClientReferenceID: in.ID, Metadata: map[string]string{"account_id": in.PersonID, "checkout_intent": in.ID, "product": in.ProductCode}}
			switch scenario {
			case "completed":
				s.Status = "complete"
			case "wrong owner":
				s.Metadata["account_id"] = "other"
			case "paid open":
				s.PaymentStatus = "paid"
			case "unknown payment":
				s.PaymentStatus = ""
			case "invoice attached":
				s.Invoice = &stripe.Invoice{ID: "in_unexpected"}
			}
			calls := 0
			lost := errors.New("lost expiration response")
			c := CheckoutClient{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return s, nil }, Expire: func(id string, p *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
				calls++
				if id != s.ID || p.IdempotencyKey == nil || *p.IdempotencyKey != "access-conflict-expire:"+in.ID {
					t.Fatal("unsafe expiration")
				}
				if scenario != "unconfirmed" {
					s.Status = "expired"
				}
				if scenario == "lost response" {
					return nil, lost
				}
				return s, nil
			}}
			err := c.ExpireConflictingCheckout(context.Background(), in, s.ID)
			if scenario == "open" {
				if err != nil || calls != 1 {
					t.Fatal(err, calls)
				}
				return
			}
			if err == nil {
				t.Fatal("unconfirmed expiration accepted")
			}
			if scenario == "completed" && !errors.Is(err, ErrReconcile) {
				t.Fatal("completed payment hidden", err)
			}
			if scenario == "lost response" {
				if err = c.ExpireConflictingCheckout(context.Background(), in, s.ID); err != nil || calls != 1 {
					t.Fatal("lost response replay", err, calls)
				}
			} else if scenario != "unconfirmed" && calls != 0 {
				t.Fatal("unsafe expiration called", calls)
			}
		})
	}
}
