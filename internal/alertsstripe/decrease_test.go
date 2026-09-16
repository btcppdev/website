package alertsstripe

import (
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

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
