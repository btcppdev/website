package alertsstripe

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestSetDecreaseSchedule(t *testing.T) {
	for _, scenario := range []string{"success", "lost-response", "foreign", "wrong-return-quantity", "wrong-return-boundary", "discount", "expired-retry", "cancelled", "wrong-attachment"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, _, now, _ := increaseFixture(t)
			q, err := c.PreviewDecrease(context.Background(), b, 1, now)
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.NewString()
			scheduleID := "sub_sched_test"
			sub.Schedule = &stripe.SubscriptionSchedule{ID: scheduleID}
			s := &stripe.SubscriptionSchedule{ID: scheduleID, Status: "active", EndBehavior: "release", Livemode: b.Live, Subscription: &stripe.Subscription{ID: b.SubscriptionID}, Customer: &stripe.Customer{ID: b.CustomerID}, CurrentPhase: &stripe.SubscriptionScheduleCurrentPhase{StartDate: q.PeriodStart, EndDate: q.EffectiveAt}, Phases: []*stripe.SubscriptionSchedulePhase{{StartDate: q.PeriodStart, EndDate: q.EffectiveAt, Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: &stripe.Price{ID: b.PriceID}, Quantity: b.PaidSlots, Metadata: map[string]string{"item-note": "keep"}}}, Metadata: map[string]string{"note": "keep"}, DefaultPaymentMethod: &stripe.PaymentMethod{ID: "pm_keep"}, Description: "Existing description"}}}
			switch scenario {
			case "foreign":
				s.Customer.ID = "cus_other"
			case "discount":
				s.Phases[0].Items[0].Discounts = []*stripe.SubscriptionSchedulePhaseItemDiscount{{}}
			case "cancelled":
				sub.CancelAtPeriodEnd = true
			case "wrong-attachment":
				sub.Schedule.ID = "sub_sched_other"
			}
			c.Schedule = func(got string, p *stripe.SubscriptionScheduleParams) (*stripe.SubscriptionSchedule, error) {
				if got != scheduleID {
					t.Fatal(got)
				}
				return s, nil
			}
			calls := 0
			lost := errors.New("response lost")
			c.UpdateSchedule = func(got string, p *stripe.SubscriptionScheduleParams) (*stripe.SubscriptionSchedule, error) {
				calls++
				if got != scheduleID || *p.IdempotencyKey != "alerts-decrease-update:"+id || *p.ProrationBehavior != "none" || *p.EndBehavior != "release" || p.DefaultSettings != nil || len(p.Phases) != 2 {
					t.Fatal(p)
				}
				if *p.Phases[0].Items[0].Quantity != 2 || *p.Phases[1].Items[0].Quantity != 1 || *p.Phases[0].EndDate != q.EffectiveAt || *p.Phases[1].StartDate != q.EffectiveAt || *p.Phases[1].Duration.Interval != "year" || *p.Phases[1].Duration.IntervalCount != 1 {
					t.Fatal(p.Phases)
				}
				s.Metadata = p.Metadata
				s.Phases = nil
				for i, phase := range p.Phases {
					if *phase.DefaultPaymentMethod != "pm_keep" || *phase.Description != "Existing description" || phase.Metadata["note"] != "keep" || phase.Items[0].Metadata["item-note"] != "keep" {
						t.Fatal(phase)
					}
					end := q.EffectiveAt
					if i == 1 {
						end = time.Unix(q.EffectiveAt, 0).AddDate(0, 1, 0).Unix()
					}
					method := stripe.SubscriptionCollectionMethod(*phase.CollectionMethod)
					s.Phases = append(s.Phases, &stripe.SubscriptionSchedulePhase{StartDate: *phase.StartDate, EndDate: end, Currency: stripe.Currency(*phase.Currency), CollectionMethod: &method, ProrationBehavior: stripe.SubscriptionSchedulePhaseProrationBehavior(*phase.ProrationBehavior), AutomaticTax: &stripe.SubscriptionAutomaticTax{Enabled: *phase.AutomaticTax.Enabled}, Metadata: phase.Metadata, DefaultPaymentMethod: &stripe.PaymentMethod{ID: *phase.DefaultPaymentMethod}, Description: *phase.Description, Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: &stripe.Price{ID: *phase.Items[0].Price}, Quantity: *phase.Items[0].Quantity, Metadata: phase.Items[0].Metadata}}})
				}
				switch scenario {
				case "wrong-return-quantity":
					s.Phases[1].Items[0].Quantity = 8
				case "wrong-return-boundary":
					s.Phases[1].StartDate++
				case "lost-response":
					return nil, lost
				}
				return s, nil
			}
			attempted := now
			if scenario == "expired-retry" {
				now = now.Add(23 * time.Hour)
			}
			got, err := c.SetDecreaseSchedule(context.Background(), b, id, scheduleID, *q, attempted, now)
			if scenario == "lost-response" {
				if !errors.Is(err, lost) {
					t.Fatal(err)
				}
				// Recovery is read-only even beyond the write retry horizon.
				got, err = c.SetDecreaseSchedule(context.Background(), b, id, scheduleID, *q, attempted, now.Add(24*time.Hour))
			}
			if scenario == "success" || scenario == "lost-response" {
				if err != nil || got == nil || calls != 1 || sub.Items.Data[0].Quantity != 2 {
					t.Fatal(got, err, calls)
				}
			} else {
				if err == nil || got != nil {
					t.Fatal("accepted invalid schedule", got, err)
				}
				if scenario != "wrong-return-quantity" && scenario != "wrong-return-boundary" && calls != 0 {
					t.Fatal("unsafe mutation")
				}
			}
		})
	}
}
