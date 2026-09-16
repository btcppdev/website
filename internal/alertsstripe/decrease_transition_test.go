package alertsstripe

import (
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestObserveDecreaseTransition(t *testing.T) {
	for _, scenario := range []string{"active", "unpaid", "released", "completed", "early-completed", "early", "old-phase", "old-quantity", "foreign", "early-release", "wrong-boundary", "different-schedule"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, _, now, _ := increaseFixture(t)
			q, err := c.PreviewDecrease(context.Background(), b, 1, now)
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.NewString()
			sid := "sub_sched_transition"
			end := time.Unix(q.EffectiveAt, 0).AddDate(0, 1, 0).Unix()
			method := stripe.SubscriptionCollectionMethod("charge_automatically")
			s := &stripe.SubscriptionSchedule{ID: sid, Status: "active", EndBehavior: "release", Livemode: b.Live, Customer: &stripe.Customer{ID: b.CustomerID}, Subscription: &stripe.Subscription{ID: b.SubscriptionID}, CurrentPhase: &stripe.SubscriptionScheduleCurrentPhase{StartDate: q.EffectiveAt, EndDate: end}, Metadata: map[string]string{"alerts_decrease": id, "account_id": b.PersonID, "checkout_intent": b.IntentID}}
			for _, quantity := range []int64{b.PaidSlots, q.PaidSlots} {
				s.Phases = append(s.Phases, &stripe.SubscriptionSchedulePhase{Currency: "usd", CollectionMethod: &method, AutomaticTax: &stripe.SubscriptionAutomaticTax{Enabled: true}, ProrationBehavior: "none", Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": "alerts.repository-slots"}, Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: &stripe.Price{ID: b.PriceID}, Quantity: quantity}}})
			}
			s.Phases[0].StartDate = q.PeriodStart
			s.Phases[0].EndDate = q.EffectiveAt
			s.Phases[1].StartDate = q.EffectiveAt
			s.Phases[1].EndDate = end
			now = time.Unix(q.EffectiveAt+1, 0)
			sub.Schedule = &stripe.SubscriptionSchedule{ID: sid}
			sub.Items.Data[0].Quantity = 1
			sub.Items.Data[0].CurrentPeriodStart = q.EffectiveAt
			sub.Items.Data[0].CurrentPeriodEnd = end
			switch scenario {
			case "unpaid":
				sub.Status = "past_due"
			case "released", "early-release", "completed", "early-completed":
				s.Status = "released"
				s.Subscription = nil
				s.CurrentPhase = nil
				s.ReleasedSubscription = &stripe.Subscription{ID: b.SubscriptionID}
				s.ReleasedAt = end
				sub.Schedule = nil
				now = time.Unix(end+1, 0)
				sub.Items.Data[0].CurrentPeriodStart = end
				sub.Items.Data[0].CurrentPeriodEnd = now.AddDate(0, 1, 0).Unix()
				if scenario == "early-release" {
					s.ReleasedAt = q.EffectiveAt - 1
				}
				if scenario == "completed" || scenario == "early-completed" {
					s.Status = "completed"
					s.ReleasedAt = 0
					s.ReleasedSubscription = nil
					s.CompletedAt = end
				}
				if scenario == "early-completed" {
					s.CompletedAt = q.EffectiveAt - 1
				}
			case "early":
				now = time.Unix(q.EffectiveAt-1, 0)
			case "old-phase":
				s.CurrentPhase.StartDate = q.PeriodStart
			case "old-quantity":
				sub.Items.Data[0].Quantity = b.PaidSlots
			case "foreign":
				s.Customer.ID = "cus_other"
			case "wrong-boundary":
				sub.Items.Data[0].CurrentPeriodStart++
			case "different-schedule":
				sub.Schedule.ID = "sub_sched_other"
			}
			reads := 0
			c.Schedule = func(got string, p *stripe.SubscriptionScheduleParams) (*stripe.SubscriptionSchedule, error) {
				reads++
				if got != sid {
					t.Fatal(got)
				}
				return s, nil
			}
			result, err := c.ObserveDecreaseTransition(context.Background(), b, id, sid, *q, now)
			if scenario == "active" || scenario == "unpaid" || scenario == "released" || scenario == "completed" {
				if err != nil || result == nil || result.Binding.PaidSlots != 1 || result.EffectiveAt != q.EffectiveAt || b.PaidSlots != 2 {
					t.Fatal(result, err)
				}
				if scenario == "unpaid" && result.State.State != "renewal_unpaid" {
					t.Fatal(result.State)
				}
			} else if err == nil || result != nil {
				t.Fatal("invalid transition", result, err)
			}
			if scenario == "early" && reads != 0 {
				t.Fatal("premature provider read")
			}
		})
	}
}
