package alertsstripe

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestPaidIncreaseInvoice(t *testing.T) {
	c, b, sub, _, invoice, now, _ := increaseFixture(t)
	q, err := c.PreviewIncrease(context.Background(), b, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	in := IncreaseIntent{ID: uuid.NewString(), Preview: *q}
	invoice.ID = "in_upgrade"
	invoice.Status = "paid"
	invoice.AmountPaid = q.TotalCents
	invoice.AmountRemaining = 0
	invoice.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: now.Add(time.Minute).Unix()}
	c.Invoice = func(id string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
		if _, ok := p.Context.Deadline(); !ok {
			t.Fatal("unbounded invoice lookup")
		}
		return invoice, nil
	}
	paid, err := c.PaidIncreaseInvoice(context.Background(), invoice.ID, b, in)
	if err != nil || paid == nil {
		t.Fatal(paid, err)
	}
	if paid.AdditionalSlots != 1 || paid.PaidSlots != 3 || paid.PreviousSlots != 2 || paid.TotalCents != 100 || paid.TaxCents != 9 || paid.QuoteID != in.ID || paid.Start.Unix() != q.ProrationDate || paid.End.Unix() != q.PeriodEnd || paid.PaidAt.Unix() != invoice.StatusTransitions.PaidAt {
		t.Fatal(paid)
	}
	if _, err = VerifyAnnualInvoice(invoice, sub.Items.Data[0].Price, b); err == nil {
		t.Fatal("upgrade accepted as full year")
	}
	if _, err = c.PaidIncreaseInvoice(context.Background(), "in_other", b, in); err == nil {
		t.Fatal("wrong lookup accepted")
	}
}

func TestIncreaseInvoiceRejectsUnpaidOrMismatchedEvidence(t *testing.T) {
	for _, scenario := range []string{"unpaid", "partial", "remaining", "paid early", "no timestamp", "preview id", "owner", "intent", "subscription", "customer", "price", "tax code", "currency", "live", "quantity", "item", "period", "total", "tax", "credit note", "balance", "discount", "extra line", "pagination", "renewal", "bad quote", "invalid slots"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, invoice, now, _ := increaseFixture(t)
			q, err := c.PreviewIncrease(context.Background(), b, 3, now)
			if err != nil {
				t.Fatal(err)
			}
			in := IncreaseIntent{ID: uuid.NewString(), Preview: *q}
			price := sub.Items.Data[0].Price
			invoice.ID = "in_upgrade"
			invoice.Status = "paid"
			invoice.AmountPaid = q.TotalCents
			invoice.AmountRemaining = 0
			invoice.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: now.Add(time.Minute).Unix()}
			switch scenario {
			case "unpaid":
				invoice.Status = "open"
			case "partial":
				invoice.AmountPaid--
			case "remaining":
				invoice.AmountRemaining = 1
			case "paid early":
				invoice.StatusTransitions.PaidAt = q.ProrationDate - 1
			case "no timestamp":
				invoice.StatusTransitions = nil
			case "preview id":
				invoice.ID = "upcoming_in_preview"
			case "owner":
				invoice.Parent.SubscriptionDetails.Metadata["account_id"] = "other"
			case "intent":
				invoice.Parent.SubscriptionDetails.Metadata["checkout_intent"] = "other"
			case "subscription":
				invoice.Parent.SubscriptionDetails.Subscription.ID = "sub_other"
			case "customer":
				invoice.Customer.ID = "cus_other"
			case "price":
				price.ID = "price_other"
			case "tax code":
				price.Product.TaxCode.ID = "txcd_merch"
			case "currency":
				invoice.Currency = "eur"
			case "live":
				invoice.Livemode = !b.Live
			case "quantity":
				invoice.Lines.Data[1].Quantity++
			case "item":
				invoice.Lines.Data[1].Parent.SubscriptionItemDetails.SubscriptionItem = "si_other"
			case "period":
				invoice.Lines.Data[1].Period.End++
			case "total":
				in.Preview.TotalCents++
			case "tax":
				in.Preview.TaxCents++
			case "credit note":
				invoice.PostPaymentCreditNotesAmount = 1
			case "balance":
				invoice.StartingBalance = 1
			case "discount":
				invoice.Discounts = []*stripe.Discount{{ID: "di_test"}}
			case "extra line":
				invoice.Lines.Data = append(invoice.Lines.Data, invoice.Lines.Data[0])
			case "pagination":
				invoice.Lines.HasMore = true
			case "renewal":
				invoice.BillingReason = "subscription_cycle"
			case "bad quote":
				in.ID = "not-a-quote"
			case "invalid slots":
				in.Preview.PreviousSlots = 0
			}
			if paid, err := VerifyIncreaseInvoice(invoice, price, b, in); err == nil || paid != nil {
				t.Fatal(paid, err)
			}
		})
	}
}
