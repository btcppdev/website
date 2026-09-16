package alertsstripe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestSubmitIncreasePaymentGate(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "applied", true: "pending"}[pending], func(t *testing.T) {
			c, b, sub, _, _, now, _ := increaseFixture(t)
			q, err := c.PreviewIncrease(context.Background(), b, 3, now)
			if err != nil {
				t.Fatal(err)
			}
			in := IncreaseIntent{ID: uuid.NewString(), Preview: *q}
			calls := 0
			c.UpdateSubscription = func(id string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
				calls++
				if id != b.SubscriptionID || p.IdempotencyKey == nil || *p.IdempotencyKey != "alerts-increase:"+in.ID || p.PaymentBehavior == nil || *p.PaymentBehavior != "pending_if_incomplete" || p.ProrationBehavior == nil || *p.ProrationBehavior != "always_invoice" || p.ProrationDate == nil || *p.ProrationDate != q.ProrationDate || len(p.Items) != 1 || *p.Items[0].ID != q.SubscriptionItemID || *p.Items[0].Quantity != 3 || p.CancelAtPeriodEnd != nil || p.BillingCycleAnchor != nil {
					t.Fatal("unsafe update parameters", p)
				}
				if _, ok := p.Context.Deadline(); !ok {
					t.Fatal("unbounded update")
				}
				sub.LatestInvoice = &stripe.Invoice{ID: "in_upgrade"}
				if pending {
					sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{ExpiresAt: now.Add(time.Hour).Unix(), SubscriptionItems: []*stripe.SubscriptionItem{{ID: q.SubscriptionItemID, Quantity: 3, Price: sub.Items.Data[0].Price}}}
				} else {
					sub.Items.Data[0].Quantity = 3
				}
				return sub, nil
			}
			out, err := c.SubmitIncrease(context.Background(), b, in, now.Add(time.Minute))
			if err != nil || out == nil || out.InvoiceID != "in_upgrade" || out.Pending != pending || calls != 1 {
				t.Fatal(out, err, calls)
			}
			// A retry must first reconcile a pending/applied provider change.
			if _, err = c.SubmitIncrease(context.Background(), b, in, now.Add(2*time.Minute)); err == nil || calls != 1 {
				t.Fatal("resubmitted changed subscription", err, calls)
			}
		})
	}
}

func TestSubmitIncreaseRejectsChangedReview(t *testing.T) {
	for _, scenario := range []string{"expired", "future", "total", "tax", "period", "item", "quantity", "owner", "pending", "unpaid"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, paid, _, now, _ := increaseFixture(t)
			q, err := c.PreviewIncrease(context.Background(), b, 3, now)
			if err != nil {
				t.Fatal(err)
			}
			in := IncreaseIntent{ID: uuid.NewString(), Preview: *q}
			switch scenario {
			case "expired":
				now = q.ExpiresAt
			case "future":
				in.Preview.ProrationDate = now.Add(time.Minute).Unix()
			case "total":
				in.Preview.TotalCents++
			case "tax":
				in.Preview.TaxCents++
			case "period":
				in.Preview.PeriodEnd++
			case "item":
				in.Preview.SubscriptionItemID = "si_other"
			case "quantity":
				in.Preview.PreviousSlots++
			case "owner":
				sub.Metadata["account_id"] = "other"
			case "pending":
				sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{}
			case "unpaid":
				paid.Status = "open"
			}
			if out, err := c.SubmitIncrease(context.Background(), b, in, now); err == nil || out != nil {
				t.Fatal(out, err)
			}
		})
	}
}

func TestSubmitIncreaseLostResponseDoesNotClaimPayment(t *testing.T) {
	c, b, _, _, _, now, _ := increaseFixture(t)
	q, err := c.PreviewIncrease(context.Background(), b, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	lost := errors.New("response lost")
	c.UpdateSubscription = func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return nil, lost }
	if out, err := c.SubmitIncrease(context.Background(), b, IncreaseIntent{ID: uuid.NewString(), Preview: *q}, now); out != nil || !errors.Is(err, lost) {
		t.Fatal(out, err)
	}
}

func TestSubmitIncreaseRejectsUnexpectedProviderResult(t *testing.T) {
	for _, scenario := range []string{"nil", "owner", "quantity", "period", "invoice", "pending item", "pending quantity", "pending expiry"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, _, now, _ := increaseFixture(t)
			q, err := c.PreviewIncrease(context.Background(), b, 3, now.Add(123*time.Millisecond))
			if err != nil {
				t.Fatal(err)
			}
			c.UpdateSubscription = func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) {
				sub.LatestInvoice = &stripe.Invoice{ID: "in_upgrade"}
				sub.Items.Data[0].Quantity = 3
				switch scenario {
				case "nil":
					return nil, nil
				case "owner":
					sub.Metadata["account_id"] = "other"
				case "quantity":
					sub.Items.Data[0].Quantity = 4
				case "period":
					sub.Items.Data[0].CurrentPeriodEnd++
				case "invoice":
					sub.LatestInvoice = nil
				default:
					sub.Items.Data[0].Quantity = 2
					sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{ExpiresAt: now.Add(time.Hour).Unix(), SubscriptionItems: []*stripe.SubscriptionItem{{ID: q.SubscriptionItemID, Quantity: 3, Price: sub.Items.Data[0].Price}}}
					switch scenario {
					case "pending item":
						sub.PendingUpdate.SubscriptionItems[0].ID = "si_other"
					case "pending quantity":
						sub.PendingUpdate.SubscriptionItems[0].Quantity = 4
					case "pending expiry":
						sub.PendingUpdate.ExpiresAt = now.Unix()
					}
				}
				return sub, nil
			}
			if out, err := c.SubmitIncrease(context.Background(), b, IncreaseIntent{ID: uuid.NewString(), Preview: *q}, now); err == nil || out != nil {
				t.Fatal(out, err)
			}
		})
	}
}
