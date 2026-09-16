package alertsstripe

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v86"
)

func increaseFixture(t *testing.T) (Client, Binding, *stripe.Subscription, *stripe.Invoice, *stripe.Invoice, time.Time, *int) {
	t.Helper()
	paid, price, b := invoiceFixture(t)
	start, end := paid.Lines.Data[0].Period.Start, paid.Lines.Data[0].Period.End
	now := time.Unix(start+(end-start)/2, 0).UTC()
	sub := &stripe.Subscription{ID: b.SubscriptionID, Customer: &stripe.Customer{ID: b.CustomerID}, Status: "active", CollectionMethod: "charge_automatically", Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": "alerts.repository-slots"}, LatestInvoice: &stripe.Invoice{ID: paid.ID}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{ID: "si_test", Quantity: 2, Price: price, CurrentPeriodStart: start, CurrentPeriodEnd: end}}}}
	raw, _ := json.Marshal(paid)
	var preview stripe.Invoice
	if err := json.Unmarshal(raw, &preview); err != nil {
		t.Fatal(err)
	}
	preview.ID = "upcoming_in_preview"
	preview.Status = "draft"
	preview.BillingReason = "subscription_update"
	preview.AmountPaid = 0
	preview.AmountDue = 100
	preview.Total = 100
	preview.TotalTaxes[0].Amount = 9
	credit := preview.Lines.Data[0]
	credit.Amount = -200
	credit.Period.Start = now.Unix()
	credit.Parent.SubscriptionItemDetails.Proration = true
	credit.Parent.SubscriptionItemDetails.SubscriptionItem = "si_test"
	debit := *credit
	debit.Amount = 300
	debit.Quantity = 3
	preview.Lines.Data = append(preview.Lines.Data, &debit)
	calls := new(int)
	c := Client{Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }, Invoice: func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return paid, nil }, Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return price, nil }, PreviewInvoice: func(p *stripe.InvoiceCreatePreviewParams) (*stripe.Invoice, error) {
		*calls++
		if p.Subscription == nil || *p.Subscription != b.SubscriptionID || p.Customer == nil || *p.Customer != b.CustomerID || p.SubscriptionDetails == nil || p.SubscriptionDetails.ProrationDate == nil || *p.SubscriptionDetails.ProrationDate != now.Unix() || p.SubscriptionDetails.ProrationBehavior == nil || *p.SubscriptionDetails.ProrationBehavior != "always_invoice" || len(p.SubscriptionDetails.Items) != 1 || *p.SubscriptionDetails.Items[0].ID != "si_test" || *p.SubscriptionDetails.Items[0].Quantity != 3 {
			t.Fatal("incorrect preview parameters", p)
		}
		if _, ok := p.Context.Deadline(); !ok {
			t.Fatal("unbounded provider request")
		}
		return &preview, nil
	}, UpdateSubscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) {
		t.Fatal("preview mutated subscription")
		return nil, nil
	}}
	return c, b, sub, paid, &preview, now, calls
}

func TestIncreasePreviewUsesPaidPeriodAndFixedProrationDate(t *testing.T) {
	c, b, _, _, _, now, calls := increaseFixture(t)
	q, err := c.PreviewIncrease(context.Background(), b, 3, now)
	if err != nil || q == nil {
		t.Fatal(q, err)
	}
	if *calls != 1 || q.PreviousSlots != 2 || q.PaidSlots != 3 || q.TotalCents != 100 || q.TaxCents != 9 || q.ProrationDate != now.Unix() || !q.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatal(q, *calls)
	}
}

func TestIncreasePreviewRejectsUnpaidAndChangingSubscriptions(t *testing.T) {
	for _, scenario := range []string{"unpaid invoice", "wrong period", "pending update", "scheduled", "cancelled", "paused", "trial", "foreign owner", "manual collection", "no increase"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, paid, _, now, calls := increaseFixture(t)
			slots := int64(3)
			switch scenario {
			case "unpaid invoice":
				paid.Status = "open"
			case "wrong period":
				sub.Items.Data[0].CurrentPeriodStart++
			case "pending update":
				sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{}
			case "scheduled":
				sub.Schedule = &stripe.SubscriptionSchedule{ID: "sub_sched_test"}
			case "cancelled":
				sub.CancelAtPeriodEnd = true
			case "paused":
				sub.PauseCollection = &stripe.SubscriptionPauseCollection{}
			case "trial":
				sub.Status = "trialing"
			case "foreign owner":
				sub.Metadata["account_id"] = "other"
			case "manual collection":
				sub.CollectionMethod = "send_invoice"
			case "no increase":
				slots = 2
			}
			if q, err := c.PreviewIncrease(context.Background(), b, slots, now); err == nil || q != nil || *calls != 0 {
				t.Fatal(q, err, *calls)
			}
		})
	}
}

func TestIncreasePreviewRejectsUnrelatedInvoiceAmounts(t *testing.T) {
	for _, scenario := range []string{"foreign customer", "foreign subscription", "wrong currency", "wrong price", "wrong item", "future period", "not proration", "extra line", "incomplete lines", "wrong quantity", "wrong sum", "credits", "exclusive tax", "failed tax", "overcharge"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, _, _, in, now, _ := increaseFixture(t)
			switch scenario {
			case "foreign customer":
				in.Customer.ID = "cus_other"
			case "foreign subscription":
				in.Parent.SubscriptionDetails.Subscription.ID = "sub_other"
			case "wrong currency":
				in.Currency = "eur"
			case "wrong price":
				in.Lines.Data[0].Pricing.PriceDetails.Price.ID = "price_other"
			case "wrong item":
				in.Lines.Data[0].Parent.SubscriptionItemDetails.SubscriptionItem = "si_other"
			case "future period":
				in.Lines.Data[0].Period.End++
			case "not proration":
				in.Lines.Data[0].Parent.SubscriptionItemDetails.Proration = false
			case "extra line":
				in.Lines.Data = append(in.Lines.Data, in.Lines.Data[0])
			case "incomplete lines":
				in.Lines.HasMore = true
			case "wrong quantity":
				in.Lines.Data[1].Quantity = 4
			case "wrong sum":
				in.Total++
			case "credits":
				in.StartingBalance = -1
			case "exclusive tax":
				in.TotalTaxes[0].TaxBehavior = "exclusive"
			case "failed tax":
				in.AutomaticTax.Status = "failed"
			case "overcharge":
				in.Total = 201
				in.AmountDue = 201
				in.Lines.Data[1].Amount = 401
			}
			if q, err := c.PreviewIncrease(context.Background(), b, 3, now); err == nil || q != nil {
				t.Fatal(q, err)
			}
		})
	}
}
