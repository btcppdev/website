package alertsstripe

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestWithdrawDecreaseSchedule(t *testing.T) {
	for _, scenario := range []string{"success", "lost-response", "foreign", "changed-quantity", "renewal-near", "expired-request", "changed-attachment", "bad-release"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, _, now, _ := increaseFixture(t)
			q, err := c.PreviewDecrease(context.Background(), b, 1, now)
			if err != nil {
				t.Fatal(err)
			}
			quoteID, withdrawalID := uuid.NewString(), uuid.NewString()
			sid := "sub_sched_withdraw"
			method := stripe.SubscriptionCollectionMethod("charge_automatically")
			s := &stripe.SubscriptionSchedule{ID: sid, Status: "active", EndBehavior: "release", Livemode: b.Live, Customer: &stripe.Customer{ID: b.CustomerID}, Subscription: &stripe.Subscription{ID: b.SubscriptionID}, CurrentPhase: &stripe.SubscriptionScheduleCurrentPhase{StartDate: q.PeriodStart, EndDate: q.EffectiveAt}, Metadata: map[string]string{"alerts_decrease": quoteID, "account_id": b.PersonID, "checkout_intent": b.IntentID}}
			for _, quantity := range []int64{b.PaidSlots, q.PaidSlots} {
				s.Phases = append(s.Phases, &stripe.SubscriptionSchedulePhase{Currency: "usd", CollectionMethod: &method, AutomaticTax: &stripe.SubscriptionAutomaticTax{Enabled: true}, ProrationBehavior: "none", Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": "alerts.repository-slots"}, Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: &stripe.Price{ID: b.PriceID}, Quantity: quantity}}})
			}
			s.Phases[0].StartDate = q.PeriodStart
			s.Phases[0].EndDate = q.EffectiveAt
			s.Phases[1].StartDate = q.EffectiveAt
			s.Phases[1].EndDate = time.Unix(q.EffectiveAt, 0).AddDate(0, 1, 0).Unix()
			sub.Schedule = &stripe.SubscriptionSchedule{ID: sid}
			requested := now
			switch scenario {
			case "foreign":
				s.Customer.ID = "cus_other"
			case "changed-quantity":
				sub.Items.Data[0].Quantity = 1
			case "renewal-near":
				now = time.Unix(q.EffectiveAt-10, 0)
			case "expired-request":
				now = now.Add(23 * time.Hour)
			case "changed-attachment":
				sub.Schedule.ID = "sub_sched_other"
			}
			c.Schedule = func(got string, p *stripe.SubscriptionScheduleParams) (*stripe.SubscriptionSchedule, error) {
				if got != sid {
					t.Fatal(got)
				}
				return s, nil
			}
			calls := 0
			lost := errors.New("response lost")
			c.ReleaseSchedule = func(got string, p *stripe.SubscriptionScheduleReleaseParams) (*stripe.SubscriptionSchedule, error) {
				calls++
				if got != sid || *p.IdempotencyKey != "alerts-decrease-withdraw:"+withdrawalID || !*p.PreserveCancelDate {
					t.Fatal(got, p)
				}
				if _, ok := p.Context.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
				s.Status = "released"
				s.ReleasedSubscription = s.Subscription
				s.Subscription = nil
				s.CurrentPhase = nil
				s.ReleasedAt = now.Unix()
				sub.Schedule = nil
				if scenario == "bad-release" {
					s.ReleasedSubscription = &stripe.Subscription{ID: "sub_other"}
				}
				if scenario == "lost-response" {
					return nil, lost
				}
				return s, nil
			}
			result, err := c.WithdrawDecreaseSchedule(context.Background(), b, quoteID, withdrawalID, sid, *q, requested, now)
			if scenario == "lost-response" {
				if !errors.Is(err, lost) {
					t.Fatal(err)
				}
				result, err = c.WithdrawDecreaseSchedule(context.Background(), b, quoteID, withdrawalID, sid, *q, requested, now.Add(time.Minute))
			}
			if scenario == "success" || scenario == "lost-response" {
				if err != nil || result.State != "active" || calls != 1 || sub.Items.Data[0].Quantity != 2 || sub.CancelAtPeriodEnd {
					t.Fatal(result, err, calls)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe withdrawal accepted")
				}
				if scenario != "bad-release" && calls != 0 {
					t.Fatal("unsafe release called")
				}
			}
		})
	}
}
