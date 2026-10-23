package alertsstripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestCardUpdateBindsSetupToSubscription(t *testing.T) {
	for _, scenario := range []string{"success", "other customer", "other subscription", "incomplete", "foreign card", "test-live mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			_, price, b := invoiceFixture(t)
			now := time.Now()
			sub := &stripe.Subscription{ID: b.SubscriptionID, Livemode: b.Live, Customer: &stripe.Customer{ID: b.CustomerID}, Status: "active", CancelAtPeriodEnd: true, Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": "alerts.repository-slots"}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: b.PaidSlots, Price: price, CurrentPeriodStart: now.Add(-time.Hour).Unix(), CurrentPeriodEnd: now.Add(time.Hour).Unix()}}}}
			setup := &stripe.SetupIntent{Status: "succeeded", Livemode: b.Live, Customer: sub.Customer, Metadata: map[string]string{"alerts_card_update": "change", "subscription": b.SubscriptionID}, PaymentMethod: &stripe.PaymentMethod{ID: "pm_new", Type: "card", Customer: sub.Customer}}
			session := &stripe.CheckoutSession{ID: "cs_setup", Mode: "setup", Status: "complete", Livemode: b.Live, Customer: sub.Customer, ClientReferenceID: "change", SetupIntent: setup}
			switch scenario {
			case "other customer":
				session.Customer = &stripe.Customer{ID: "cus_foreign"}
			case "other subscription":
				setup.Metadata["subscription"] = "sub_other"
			case "incomplete":
				setup.Status = "requires_action"
			case "foreign card":
				setup.PaymentMethod.Customer = &stripe.Customer{ID: "cus_other"}
			case "test-live mismatch":
				session.Livemode = !b.Live
			}
			updates := 0
			client := Client{Live: b.Live, Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }, UpdateSubscription: func(id string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
				updates++
				if id != b.SubscriptionID || p.DefaultPaymentMethod == nil || *p.DefaultPaymentMethod != "pm_new" || p.CancelAtPeriodEnd != nil || len(p.Items) > 0 || p.BillingCycleAnchor != nil {
					t.Fatal("unsafe card update", p)
				}
				sub.DefaultPaymentMethod = setup.PaymentMethod
				return sub, nil
			}}
			err := client.ApplyCardUpdate(context.Background(), b, "change", "cs_setup")
			if scenario == "success" {
				if err != nil || updates != 1 {
					t.Fatal(err, updates)
				}
			} else if err == nil || updates != 0 {
				t.Fatal("unverified card accepted", err, updates)
			}
		})
	}
}
