package servicestripe

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestFindPaymentCheckout(t *testing.T) {
	for _, scenario := range []string{"unique", "paid", "none", "duplicate", "wrong order", "wrong total", "wrong mode", "wrong live", "wrong kind", "outside window", "repeated page", "empty continuation", "failure after match"} {
		t.Run(scenario, func(t *testing.T) {
			in := PaymentCheckout{AttemptID: uuid.NewString(), OrderID: uuid.NewString(), TotalCents: 200}
			created := time.Now().UTC()
			s := &stripe.CheckoutSession{ID: "cs_found", Mode: "payment", Currency: "usd", AmountTotal: 200, Created: created.Unix(), Metadata: map[string]string{"service-attempt-id": in.AttemptID, "service-order-id": in.OrderID, "checkout-kind": "service"}}
			calls := 0
			c := CheckoutClient{List: func(p *stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
				calls++
				if p.Context == nil || p.Limit == nil || *p.Limit != 100 || !p.Single || p.CreatedRange == nil || p.CreatedRange.GreaterThanOrEqual != created.Add(-time.Minute).Unix() || p.CreatedRange.LesserThanOrEqual != created.Add(2*time.Hour+time.Minute).Unix() {
					t.Fatal("unbounded scan")
				}
				if calls == 1 {
					return &stripe.CheckoutSessionList{ListMeta: stripe.ListMeta{HasMore: true}, Data: []*stripe.CheckoutSession{{ID: "cs_unrelated"}}}, nil
				}
				if calls == 2 && (p.StartingAfter == nil || *p.StartingAfter != "cs_unrelated") {
					t.Fatal("missing cursor")
				}
				switch scenario {
				case "paid":
					s.Status = "complete"
					s.PaymentStatus = "paid"
				case "none":
					return &stripe.CheckoutSessionList{}, nil
				case "duplicate":
					other := *s
					other.ID = "cs_duplicate"
					return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{s, &other}}, nil
				case "wrong order":
					s.Metadata["service-order-id"] = uuid.NewString()
				case "wrong total":
					s.AmountTotal++
				case "wrong mode":
					s.Mode = "subscription"
				case "wrong live":
					s.Livemode = true
				case "wrong kind":
					s.Metadata["checkout-kind"] = "shop"
				case "outside window":
					s.Created = created.Add(24 * time.Hour).Unix()
				case "repeated page":
					return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{{ID: "cs_unrelated"}}}, nil
				case "empty continuation":
					return &stripe.CheckoutSessionList{ListMeta: stripe.ListMeta{HasMore: true}}, nil
				case "failure after match":
					if calls == 2 {
						return &stripe.CheckoutSessionList{ListMeta: stripe.ListMeta{HasMore: true}, Data: []*stripe.CheckoutSession{s}}, nil
					}
					return nil, context.DeadlineExceeded
				}
				return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{s}}, nil
			}}
			got, err := c.FindPaymentCheckout(context.Background(), in, created, created.Add(2*time.Hour))
			if scenario == "unique" || scenario == "paid" {
				if err != nil || got != "cs_found" {
					t.Fatal(got, err)
				}
			} else if err == nil || got != "" {
				t.Fatal("ambiguous lookup accepted", got, err)
			}
		})
	}
}
