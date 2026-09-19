package servicestripe

import (
	"context"
	"errors"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestDeletedCheckoutExpirationVerification(t *testing.T) {
	for _, scenario := range []string{"lost response", "wrong account", "wrong session", "wrong mode", "unexpected subscription", "unconfirmed expiration"} {
		t.Run(scenario, func(t *testing.T) {
			in := insiderIntent("insider.membership.monthly")
			s := &stripe.CheckoutSession{ID: "cs_test", Mode: "subscription", Status: "open", ClientReferenceID: in.ID, Metadata: map[string]string{"account_id": in.PersonID, "checkout_intent": in.ID, "product": in.ProductCode}}
			switch scenario {
			case "wrong account":
				s.Metadata["account_id"] = "other"
			case "wrong session":
				s.ID = "cs_other"
			case "wrong mode":
				s.Livemode = true
			case "unexpected subscription":
				s.Subscription = &stripe.Subscription{ID: "sub_unexpected"}
			}
			calls := 0
			lost := errors.New("lost expiration response")
			c := CheckoutClient{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return s, nil }, Expire: func(id string, p *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
				calls++
				if id != "cs_test" || p.IdempotencyKey == nil || *p.IdempotencyKey != "deleted-account-expire:"+in.ID {
					t.Fatal("unsafe expiration request")
				}
				if scenario == "lost response" {
					s.Status = "expired"
					return nil, lost
				}
				return s, nil
			}}
			if _, err := c.ResolveDeletedCheckout(context.Background(), in, "cs_test"); err == nil {
				t.Fatal("unconfirmed expiration accepted")
			}
			if scenario == "lost response" {
				if b, err := c.ResolveDeletedCheckout(context.Background(), in, "cs_test"); err != nil || b != nil || calls != 1 {
					t.Fatal("expiration retry", b, err, calls)
				}
			} else if scenario != "unconfirmed expiration" && calls != 0 {
				t.Fatal("mismatched checkout expired", calls)
			}
		})
	}
}
