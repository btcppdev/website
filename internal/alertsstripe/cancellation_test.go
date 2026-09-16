package alertsstripe

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestScheduleCancellation(t *testing.T) {
	for _, scenario := range []string{"success", "already scheduled", "other account", "stale period", "expired request", "custom date", "pending change", "managed schedule", "provider error", "unconfirmed response", "changed response period"} {
		t.Run(scenario, func(t *testing.T) {
			_, price, b := invoiceFixture(t)
			now := time.Now().UTC()
			in := CancellationIntent{ID: uuid.NewString(), PeriodEnd: now.Add(24 * time.Hour).Unix(), CreatedAt: now}
			sub := &stripe.Subscription{ID: b.SubscriptionID, Livemode: b.Live, Customer: &stripe.Customer{ID: b.CustomerID}, Status: "active", Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": "alerts.repository-slots"}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: b.PaidSlots, Price: price, CurrentPeriodStart: now.Add(-24 * time.Hour).Unix(), CurrentPeriodEnd: in.PeriodEnd}}}}
			switch scenario {
			case "already scheduled":
				sub.CancelAtPeriodEnd = true
				sub.CancelAt = in.PeriodEnd
			case "other account":
				sub.Customer.ID = "cus_other"
			case "stale period":
				in.PeriodEnd++
			case "expired request":
				in.CreatedAt = now.Add(-24 * time.Hour)
			case "custom date":
				sub.CancelAt = in.PeriodEnd - 3600
			case "pending change":
				sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{}
			case "managed schedule":
				sub.Schedule = &stripe.SubscriptionSchedule{ID: "sub_sched_test"}
			}
			updates := 0
			providerErr := errors.New("connection interrupted")
			c := Client{Live: b.Live, Subscription: func(id string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }, UpdateSubscription: func(id string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
				updates++
				if id != b.SubscriptionID || p.CancelAtPeriodEnd == nil || !*p.CancelAtPeriodEnd || p.IdempotencyKey == nil || *p.IdempotencyKey != "alerts-cancel:"+in.ID || p.Items != nil || p.ProrationBehavior != nil || p.CancelAt != nil {
					t.Fatal("unsafe cancellation parameters", p)
				}
				if scenario == "provider error" {
					return nil, providerErr
				}
				if scenario != "unconfirmed response" {
					sub.CancelAtPeriodEnd = true
					sub.CancelAt = in.PeriodEnd
				}
				if scenario == "changed response period" {
					sub.Items.Data[0].CurrentPeriodEnd++
				}
				return sub, nil
			}}
			got, err := c.ScheduleCancellation(context.Background(), b, in, now)
			if scenario == "success" || scenario == "already scheduled" {
				if err != nil || !got.CancelAtPeriodEnd || got.PeriodEnd != in.PeriodEnd {
					t.Fatal(got, err)
				}
				if scenario == "already scheduled" && updates != 0 {
					t.Fatal("duplicate mutation")
				}
				if _, err = c.ScheduleCancellation(context.Background(), b, in, now); err != nil {
					t.Fatal("replay", err)
				}
				if scenario == "success" && updates != 1 {
					t.Fatal("replay mutated subscription", updates)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe/unconfirmed cancellation accepted")
				}
				if scenario == "provider error" && !errors.Is(err, providerErr) {
					t.Fatal("lost ambiguous failure", err)
				}
				if scenario != "provider error" && scenario != "unconfirmed response" && scenario != "changed response period" && updates != 0 {
					t.Fatal("mutated before verification")
				}
			}
		})
	}
}
