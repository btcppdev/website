package servicestripe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestDeletedAccountCancellationRecoversLostReply(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.monthly", "insider.membership.annual"} {
		t.Run(product, func(t *testing.T) {
			_, price, b := invoiceFixture(t)
			if product != "alerts.repository-slots" {
				_, price, b = insiderInvoice(t, product)
			}
			b.IntentID = uuid.NewString()
			now := time.Now().UTC()
			sub := &stripe.Subscription{ID: b.SubscriptionID, Livemode: b.Live, Customer: &stripe.Customer{ID: b.CustomerID}, Status: "active", Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": b.ProductCode}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: b.Quantity, Price: price, CurrentPeriodStart: now.Add(-time.Hour).Unix(), CurrentPeriodEnd: now.Add(10 * 24 * time.Hour).Unix()}}}}
			calls := 0
			var key string
			lost := errors.New("lost provider response")
			client := SubscriptionClient{Live: b.Live, Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }, UpdateSubscription: func(id string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
				calls++
				if id != b.SubscriptionID || p.CancelAtPeriodEnd == nil || !*p.CancelAtPeriodEnd || p.Items != nil || p.ProrationBehavior != nil || p.CancelAt != nil || p.IdempotencyKey == nil {
					t.Fatal("unsafe deletion cancellation", p)
				}
				key = *p.IdempotencyKey
				sub.CancelAtPeriodEnd = true
				sub.CancelAt = sub.Items.Data[0].CurrentPeriodEnd
				return nil, lost
			}}
			if _, err := client.ScheduleDeletedAccountCancellation(context.Background(), b, now); !errors.Is(err, lost) {
				t.Fatal(err)
			}
			state, err := client.ScheduleDeletedAccountCancellation(context.Background(), b, now.Add(48*time.Hour))
			if err != nil || !state.CancelAtPeriodEnd || calls != 1 || key == "" {
				t.Fatal("lost reply not reconciled", state, calls, err)
			}
			sub.Status = "canceled"
			if state, err := client.ScheduleDeletedAccountCancellation(context.Background(), b, now.Add(12*24*time.Hour)); err != nil || state.ProviderStatus != "canceled" || calls != 1 {
				t.Fatal("terminal subscription mutated", state, err, calls)
			}
			sub.Customer.ID = "cus_other"
			if _, err := client.ScheduleDeletedAccountCancellation(context.Background(), b, now); err == nil || calls != 1 {
				t.Fatal("other customer accepted", err, calls)
			}
		})
	}
}

func TestDeletedAccountCancellationRefreshesPeriodAfterOutage(t *testing.T) {
	_, price, b := insiderInvoice(t, "insider.membership.monthly")
	b.IntentID = uuid.NewString()
	now := time.Now().UTC()
	sub := &stripe.Subscription{ID: b.SubscriptionID, Livemode: b.Live, Customer: &stripe.Customer{ID: b.CustomerID}, Status: "active", Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": b.ProductCode}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: b.Quantity, Price: price, CurrentPeriodStart: now.Add(-time.Hour).Unix(), CurrentPeriodEnd: now.Add(24 * time.Hour).Unix()}}}}
	var keys []string
	client := SubscriptionClient{Live: b.Live, Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }, UpdateSubscription: func(_ string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
		keys = append(keys, *p.IdempotencyKey)
		return nil, errors.New("provider unavailable")
	}}
	for i := 0; i < 2; i++ {
		if _, err := client.ScheduleDeletedAccountCancellation(context.Background(), b, now); err == nil {
			t.Fatal("unconfirmed cancellation accepted")
		}
	}
	sub.Items.Data[0].CurrentPeriodStart = now.Add(24 * time.Hour).Unix()
	sub.Items.Data[0].CurrentPeriodEnd = now.Add(31 * 24 * time.Hour).Unix()
	if _, err := client.ScheduleDeletedAccountCancellation(context.Background(), b, now.Add(48*time.Hour)); err == nil {
		t.Fatal("unconfirmed later cancellation accepted")
	}
	if len(keys) != 3 || keys[0] != keys[1] || keys[1] == keys[2] {
		t.Fatal("unstable period-specific idempotency", keys)
	}
}
