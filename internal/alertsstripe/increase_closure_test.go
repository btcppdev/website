package alertsstripe

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestVoidPendingIncrease(t *testing.T) {
	for _, scenario := range []string{"void", "lost response", "paid", "partial", "different latest", "different quantity", "applied", "scheduled", "foreign", "still pending"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, invoice, now, _ := increaseFixture(t)
			q, err := c.PreviewIncrease(context.Background(), b, 3, now)
			if err != nil {
				t.Fatal(err)
			}
			intent := IncreaseIntent{ID: uuid.NewString(), Preview: *q}
			invoice.ID = "in_closure"
			invoice.Status = "open"
			invoice.StatusTransitions = nil
			invoice.AmountRemaining = invoice.Total
			c.Invoice = func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return invoice, nil }
			sub.LatestInvoice = &stripe.Invoice{ID: invoice.ID}
			sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{SubscriptionItems: []*stripe.SubscriptionItem{{ID: q.SubscriptionItemID, Quantity: q.PaidSlots, Price: sub.Items.Data[0].Price}}}
			calls := 0
			c.VoidInvoice = func(id string, p *stripe.InvoiceVoidInvoiceParams) (*stripe.Invoice, error) {
				calls++
				if id != invoice.ID || p.IdempotencyKey == nil || *p.IdempotencyKey != "alerts-closure-void-increase:"+intent.ID || p.Context == nil {
					t.Fatal("unfenced void")
				}
				invoice.Status = "void"
				invoice.AmountRemaining = 0
				if scenario != "still pending" {
					sub.PendingUpdate = nil
				}
				if scenario == "lost response" {
					return nil, errors.New("response lost")
				}
				return invoice, nil
			}
			switch scenario {
			case "paid":
				invoice.Status = "paid"
				invoice.AmountPaid = invoice.Total
				invoice.AmountRemaining = 0
			case "partial":
				invoice.AmountPaid = 1
				invoice.AmountRemaining--
			case "different latest":
				sub.LatestInvoice.ID = "in_other"
			case "different quantity":
				sub.PendingUpdate.SubscriptionItems[0].Quantity++
			case "applied":
				sub.Items.Data[0].Quantity = q.PaidSlots
			case "scheduled":
				sub.Schedule = &stripe.SubscriptionSchedule{ID: "sub_sched_other"}
			case "foreign":
				sub.Metadata["account_id"] = "other"
			}
			err = c.VoidPendingIncrease(context.Background(), invoice.ID, b, intent)
			if scenario == "void" {
				if err != nil || calls != 1 {
					t.Fatal(err, calls)
				}
			} else if err == nil {
				t.Fatal("unsafe closure accepted")
			}
			if scenario == "lost response" {
				if err = c.VoidPendingIncrease(context.Background(), invoice.ID, b, intent); err != nil || calls != 1 {
					t.Fatal("lost-response recovery", err, calls)
				}
				if _, err = c.VoidedIncrease(context.Background(), invoice.ID, b, intent); err != nil {
					t.Fatal(err)
				}
			} else if scenario != "void" && scenario != "still pending" && calls != 0 {
				t.Fatal("unsafe invoice voided")
			}
		})
	}
}
