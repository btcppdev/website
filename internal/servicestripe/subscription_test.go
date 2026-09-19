package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestInsiderSubscriptionLifecycleDistinctions(t *testing.T) {
	_, price, b := insiderInvoice(t, "insider.membership.monthly")
	for _, tc := range []struct {
		status, reason string
		pause, cancel  bool
		want           string
	}{
		{"active", "", false, false, "active"},
		{"active", "", true, false, "paused"},
		{"paused", "", false, false, "paused"},
		{"active", "", false, true, "cancelling"},
		{"past_due", "", false, false, "renewal_unpaid"},
		{"unpaid", "", false, false, "renewal_unpaid"},
		{"past_due", "", true, false, "paused"},
		{"past_due", "", false, true, "cancelling"},
		{"canceled", "cancellation_requested", false, false, "cancelled"},
		{"canceled", "payment_failed", false, false, "renewal_unpaid"},
		{"canceled", "payment_disputed", false, false, "disputed"},
		{"incomplete", "", false, false, "initial_unpaid"},
		{"incomplete_expired", "", false, false, "expired"},
	} {
		s := &stripe.Subscription{ID: b.SubscriptionID, Customer: &stripe.Customer{ID: b.CustomerID}, Status: stripe.SubscriptionStatus(tc.status), CancelAtPeriodEnd: tc.cancel, Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": b.ProductCode}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: b.Quantity, Price: price, CurrentPeriodStart: 100, CurrentPeriodEnd: 200}}}}
		if tc.pause {
			s.PauseCollection = &stripe.SubscriptionPauseCollection{Behavior: "void"}
		}
		if tc.reason != "" {
			s.CancellationDetails = &stripe.SubscriptionCancellationDetails{Reason: stripe.SubscriptionCancellationDetailsReason(tc.reason)}
		}
		c := SubscriptionClient{Subscription: func(_ string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
			if len(p.Expand) != 1 {
				t.Fatal("missing price expansion")
			}
			return s, nil
		}}
		out, err := c.CurrentSubscription(context.Background(), b)
		if err != nil || out.State != tc.want {
			t.Fatal(tc, out, err)
		}
		s.Customer.ID = "cus_other"
		if _, err = c.CurrentSubscription(context.Background(), b); err == nil {
			t.Fatal("wrong account accepted")
		}
	}
}
