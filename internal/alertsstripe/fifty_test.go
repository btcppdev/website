package alertsstripe

import (
	"btcpp-web/internal/servicebilling"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func fiftyInvoice(t *testing.T) (*stripe.Invoice, *stripe.Price, Binding) {
	inv, price, b := invoiceFixture(t)
	b.ProductCode = servicebilling.AlertsFiftyProduct
	b.PaidSlots = 1
	price.UnitAmount = 18000
	price.Recurring.Interval = "year"
	inv.Total = 18000
	inv.AmountPaid = 18000
	inv.Lines.Data[0].Amount = 18000
	inv.Lines.Data[0].Quantity = 1
	inv.Parent.SubscriptionDetails.Metadata["product"] = b.Product()
	start := time.Date(2027, 1, 31, 12, 0, 0, 0, time.UTC)
	end := time.Date(2028, 1, 31, 12, 0, 0, 0, time.UTC)
	inv.Lines.Data[0].Period.Start = start.Unix()
	inv.Lines.Data[0].Period.End = end.Unix()
	inv.StatusTransitions.PaidAt = start.Unix()
	return inv, price, b
}
func TestFiftyAnnualInvoiceAndFailedRenewal(t *testing.T) {
	inv, price, b := fiftyInvoice(t)
	paid, err := VerifyAnnualInvoice(inv, price, b)
	if err != nil || paid.Quantity != 1 || paid.TotalCents != 18000 {
		t.Fatal(paid, err)
	}
	for _, mutate := range []func(*stripe.Invoice, *stripe.Price){
		func(i *stripe.Invoice, p *stripe.Price) { p.Recurring.Interval = "month" },
		func(i *stripe.Invoice, p *stripe.Price) { i.Lines.Data[0].Period.End += 300 * 86400 },
		func(i *stripe.Invoice, p *stripe.Price) {
			i.Total = 36000
			i.AmountPaid = 36000
			i.Lines.Data[0].Amount = 36000
			i.Lines.Data[0].Quantity = 2
		},
		func(i *stripe.Invoice, p *stripe.Price) {
			i.Parent.SubscriptionDetails.Metadata["product"] = servicebilling.AlertsProduct
		},
	} {
		bad, p, b := fiftyInvoice(t)
		mutate(bad, p)
		if _, err := VerifyAnnualInvoice(bad, p, b); err == nil {
			t.Fatal("accepted mismatched annual billing")
		}
	}
	inv.Status = "open"
	inv.AmountPaid = 0
	inv.AmountRemaining = 18000
	inv.Attempted = true
	inv.AttemptCount = 1
	inv.CollectionMethod = "charge_automatically"
	inv.StatusTransitions = nil
	previous := *paid
	previous.InvoiceID = "in_previous"
	previous.End = paid.Start
	previous.Start = paid.Start.AddDate(-1, 0, 0)
	previous.PaidAt = previous.Start
	failure, err := VerifyFailedRenewal(inv, price, b, previous, paid.Start.Add(time.Hour))
	if err != nil || failure.Quantity != 1 || !failure.DueAt.Equal(paid.Start) {
		t.Fatal(failure, err)
	}
}
