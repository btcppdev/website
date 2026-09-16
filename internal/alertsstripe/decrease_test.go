package alertsstripe

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestDecreaseScheduleAttachment(t *testing.T) {
	for _, scenario := range []string{"retry", "resume", "retry-expired", "retry-future", "retry-late-consent", "retry-renewed", "customer", "subscription", "mode", "quantity", "price", "period", "cancel", "existing", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, _, now, _ := increaseFixture(t)
			q, err := c.PreviewDecrease(context.Background(), b, 1, now)
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.NewString()
			firstAttempt := now
			s := &stripe.SubscriptionSchedule{ID: "sub_sched_test", Livemode: b.Live, Status: "active", EndBehavior: "release", Customer: &stripe.Customer{ID: b.CustomerID}, Subscription: &stripe.Subscription{ID: b.SubscriptionID}, CurrentPhase: &stripe.SubscriptionScheduleCurrentPhase{StartDate: q.PeriodStart, EndDate: q.EffectiveAt}, Phases: []*stripe.SubscriptionSchedulePhase{{StartDate: q.PeriodStart, EndDate: q.EffectiveAt, Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: &stripe.Price{ID: b.PriceID}, Quantity: b.PaidSlots}}}}}
			switch scenario {
			case "customer":
				s.Customer.ID = "cus_other"
			case "subscription":
				s.Subscription.ID = "sub_other"
			case "mode":
				s.Livemode = !b.Live
			case "quantity":
				s.Phases[0].Items[0].Quantity--
			case "price":
				s.Phases[0].Items[0].Price.ID = "price_other"
			case "period":
				s.CurrentPhase.EndDate++
			case "cancel":
				s.EndBehavior = "cancel"
			case "existing":
				sub.Schedule = &stripe.SubscriptionSchedule{ID: "sub_sched_other"}
			case "expired":
				now = q.ExpiresAt
			case "resume":
				now = now.Add(11 * time.Minute)
			case "retry-expired":
				now = now.Add(23 * time.Hour)
			case "retry-future":
				firstAttempt = now.Add(time.Minute)
			case "retry-late-consent":
				firstAttempt = q.ExpiresAt
				now = q.ExpiresAt.Add(time.Minute)
			case "retry-renewed":
				now = time.Unix(q.EffectiveAt, 0)
			}
			calls := 0
			lost := errors.New("response lost")
			c.CreateSchedule = func(p *stripe.SubscriptionScheduleParams) (*stripe.SubscriptionSchedule, error) {
				calls++
				if p.FromSubscription == nil || *p.FromSubscription != b.SubscriptionID || p.IdempotencyKey == nil || *p.IdempotencyKey != "alerts-decrease-create:"+id || len(p.Phases) != 0 || len(p.Metadata) != 0 {
					t.Fatal(p)
				}
				if _, ok := p.Context.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
				if scenario == "retry" && calls == 1 {
					sub.Schedule = &stripe.SubscriptionSchedule{ID: s.ID}
					return nil, lost
				}
				return s, nil
			}
			var got string
			if scenario == "resume" || scenario == "retry-expired" || scenario == "retry-future" || scenario == "retry-late-consent" || scenario == "retry-renewed" {
				got, err = c.ResumeDecreaseSchedule(context.Background(), b, id, *q, firstAttempt, now)
			} else {
				got, err = c.AttachDecreaseSchedule(context.Background(), b, id, *q, now)
			}
			if scenario == "retry" {
				if !errors.Is(err, lost) {
					t.Fatal(got, err)
				}
				got, err = c.AttachDecreaseSchedule(context.Background(), b, id, *q, now)
				if err != nil || got != s.ID || calls != 2 || sub.Items.Data[0].Quantity != b.PaidSlots {
					t.Fatal(got, err, calls)
				}
			} else if scenario == "resume" {
				if err != nil || got != s.ID || calls != 1 {
					t.Fatal(got, err, calls)
				}
			} else if err == nil || got != "" {
				t.Fatal("accepted invalid schedule", got, err)
			}
			if scenario == "expired" && calls != 0 {
				t.Fatal("expired consent created schedule")
			}
			if (scenario == "retry-expired" || scenario == "retry-future" || scenario == "retry-late-consent" || scenario == "retry-renewed") && calls != 0 {
				t.Fatal("unsafe retry reached provider")
			}
		})
	}
}

func TestDecreaseKeepsCurrentQuantityUntilRenewal(t *testing.T) {
	c, b, _, _, _, now, _ := increaseFixture(t)
	q, err := c.PreviewDecrease(context.Background(), b, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if q.PreviousSlots != 2 || q.PaidSlots != 1 || q.MonthlyCents != 200 {
		t.Fatal(q)
	}
	id := uuid.NewString()
	p, err := decreaseScheduleParams(context.Background(), b, id, *q, now)
	if err != nil {
		t.Fatal(err)
	}
	if *p.ProrationBehavior != "none" || *p.EndBehavior != "release" || len(p.Phases) != 2 || *p.IdempotencyKey != "alerts-decrease-update:"+id {
		t.Fatal(p)
	}
	current, next := p.Phases[0], p.Phases[1]
	if *current.Items[0].Quantity != 2 || *next.Items[0].Quantity != 1 || *current.EndDate != q.EffectiveAt || *next.StartDate != q.EffectiveAt || *next.Duration.Interval != "month" || *next.Duration.IntervalCount != 1 {
		t.Fatal(current, next)
	}
	for _, phase := range p.Phases {
		if *phase.Items[0].Price != b.PriceID || !*phase.AutomaticTax.Enabled || *phase.ProrationBehavior != "none" || phase.Metadata["account_id"] != b.PersonID {
			t.Fatal(phase)
		}
	}
	zero, err := c.PreviewDecrease(context.Background(), b, 0, now)
	if err != nil || zero.MonthlyCents != 0 {
		t.Fatal(zero, err)
	}
	if _, err = decreaseScheduleParams(context.Background(), b, id, *zero, now); err == nil {
		t.Fatal("zero slots must use cancellation")
	}
	if _, err = decreaseScheduleParams(context.Background(), b, id, *q, q.ExpiresAt); err == nil {
		t.Fatal("expired review accepted")
	}
}

func TestDecreaseReviewRejectsConflictingState(t *testing.T) {
	for _, scenario := range []string{"same", "increase", "negative", "pending", "scheduled", "cancelled", "paused", "foreign", "manual", "discount"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, _, now, _ := increaseFixture(t)
			slots := int64(1)
			switch scenario {
			case "same":
				slots = 2
			case "increase":
				slots = 3
			case "negative":
				slots = -1
			case "pending":
				sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{}
			case "scheduled":
				sub.Schedule = &stripe.SubscriptionSchedule{ID: "sub_sched_other"}
			case "cancelled":
				sub.CancelAtPeriodEnd = true
			case "paused":
				sub.PauseCollection = &stripe.SubscriptionPauseCollection{}
			case "foreign":
				sub.Metadata["account_id"] = "other"
			case "manual":
				sub.CollectionMethod = "send_invoice"
			case "discount":
				sub.Discounts = []*stripe.Discount{{ID: "di_test"}}
			}
			if q, err := c.PreviewDecrease(context.Background(), b, slots, now); err == nil || q != nil {
				t.Fatal(q, err)
			}
		})
	}
}
