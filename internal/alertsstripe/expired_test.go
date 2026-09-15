package alertsstripe

import (
	"context"
	"errors"
	"testing"

	"github.com/stripe/stripe-go/v86"
)

func TestExpiredUnpaidRequiresAuthenticatedTerminalState(t *testing.T) {
	in := testIntent()
	for _, tc := range []struct {
		name   string
		change func(*stripe.CheckoutSession)
		ok     bool
	}{
		{"expired", func(*stripe.CheckoutSession) {}, true},
		{"open", func(s *stripe.CheckoutSession) { s.Status = "open" }, false},
		{"complete", func(s *stripe.CheckoutSession) { s.Status = "complete" }, false},
		{"paid", func(s *stripe.CheckoutSession) { s.PaymentStatus = "paid" }, false},
		{"unknown payment", func(s *stripe.CheckoutSession) { s.PaymentStatus = "" }, false},
		{"subscription", func(s *stripe.CheckoutSession) { s.Subscription = &stripe.Subscription{ID: "sub_paid"} }, false},
		{"invoice", func(s *stripe.CheckoutSession) { s.Invoice = &stripe.Invoice{ID: "in_paid"} }, false},
		{"wrong account", func(s *stripe.CheckoutSession) { s.Metadata["account_id"] = "other" }, false},
		{"wrong session", func(s *stripe.CheckoutSession) { s.ID = "cs_other" }, false},
		{"wrong mode", func(s *stripe.CheckoutSession) { s.Livemode = true }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &stripe.CheckoutSession{ID: "cs_expired", Mode: "subscription", Status: "expired", PaymentStatus: "unpaid", ClientReferenceID: in.ID, Metadata: map[string]string{"account_id": in.PersonID, "checkout_intent": in.ID, "product": "alerts.repository-slots"}}
			tc.change(s)
			c := Client{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return s, nil }}
			if err := c.ExpiredUnpaid(context.Background(), in, "cs_expired"); (err == nil) != tc.ok {
				t.Fatal(err)
			}
		})
	}
	c := Client{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		return nil, errors.New("offline")
	}}
	if err := c.ExpiredUnpaid(context.Background(), in, "cs_expired"); err == nil {
		t.Fatal("lookup failure allowed closure")
	}
}
