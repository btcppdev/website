package alertsstripe

import (
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestLatestIncreaseInvoice(t *testing.T) {
	for _, scenario := range []string{"open", "paid", "void", "foreign subscription", "foreign invoice", "wrong quantity", "wrong totals", "draft", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, invoice, now, _ := increaseFixture(t)
			q, err := c.PreviewIncrease(context.Background(), b, 3, now)
			if err != nil {
				t.Fatal(err)
			}
			intent := IncreaseIntent{ID: uuid.NewString(), Preview: *q}
			invoice.ID = "in_upgrade"
			invoice.Status = "open"
			invoice.AmountRemaining = invoice.Total
			invoice.StatusTransitions = nil
			sub.LatestInvoice = &stripe.Invoice{ID: invoice.ID}
			c.Invoice = func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return invoice, nil }
			switch scenario {
			case "paid":
				invoice.Status = "paid"
				invoice.AmountPaid = invoice.Total
				invoice.AmountRemaining = 0
				invoice.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}
				sub.Items.Data[0].Quantity = 3
			case "void":
				invoice.Status = "void"
				invoice.AmountRemaining = 0
			case "foreign subscription":
				sub.Metadata["account_id"] = "other"
			case "foreign invoice":
				invoice.Parent.SubscriptionDetails.Metadata["account_id"] = "other"
			case "wrong quantity":
				sub.Items.Data[0].Quantity = 4
			case "wrong totals":
				invoice.Total++
			case "draft":
				invoice.Status = "draft"
			case "partial":
				invoice.AmountPaid = 1
				invoice.AmountRemaining--
			}
			observed, err := c.LatestIncreaseInvoice(context.Background(), b, intent)
			valid := scenario == "open" || scenario == "paid" || scenario == "void"
			if valid {
				if err != nil || observed == nil || observed.InvoiceID != invoice.ID || observed.Status != string(invoice.Status) {
					t.Fatal(observed, err)
				}
			} else if err == nil || observed != nil {
				t.Fatal("invalid observation", observed, err)
			}
		})
	}
}
