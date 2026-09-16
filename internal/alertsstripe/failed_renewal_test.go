package alertsstripe

import (
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestFailedRenewalEligibility(t *testing.T) {
	fixture := func() (*stripe.Invoice, *stripe.Price, Binding, PaidPeriod, time.Time) {
		inv, price, b := invoiceFixture(t)
		inv.Status = "open"
		inv.AmountPaid = 0
		inv.AmountRemaining = inv.Total
		inv.StatusTransitions.PaidAt = 0
		inv.Attempted = true
		inv.AttemptCount = 1
		inv.CollectionMethod = "charge_automatically"
		due := time.Unix(inv.Lines.Data[0].Period.Start, 0).UTC()
		prior := PaidPeriod{InvoiceID: "in_previous", SubscriptionID: b.SubscriptionID, Quantity: b.PaidSlots, Start: due.AddDate(0, -1, 0), End: due, PaidAt: due.AddDate(0, -1, 0)}
		return inv, price, b, prior, due.Add(time.Hour)
	}
	inv, price, b, prior, now := fixture()
	first, err := VerifyFailedRenewal(inv, price, b, prior, now)
	if err != nil {
		t.Fatal(err)
	}
	inv.AttemptCount = 8
	second, err := VerifyFailedRenewal(inv, price, b, prior, now.Add(10*24*time.Hour))
	if err != nil || !first.DueAt.Equal(second.DueAt) {
		t.Fatal("retry extended grace", second, err)
	}
	for name, change := range map[string]func(*stripe.Invoice, *PaidPeriod){
		"initial invoice":     func(i *stripe.Invoice, p *PaidPeriod) { i.BillingReason = "subscription_create" },
		"not attempted":       func(i *stripe.Invoice, p *PaidPeriod) { i.Attempted = false },
		"zero attempts":       func(i *stripe.Invoice, p *PaidPeriod) { i.AttemptCount = 0 },
		"already paid":        func(i *stripe.Invoice, p *PaidPeriod) { i.Status = "paid" },
		"void invoice":        func(i *stripe.Invoice, p *PaidPeriod) { i.Status = "void" },
		"manual invoice":      func(i *stripe.Invoice, p *PaidPeriod) { i.CollectionMethod = "send_invoice" },
		"no prior payment":    func(i *stripe.Invoice, p *PaidPeriod) { p.PaidAt = time.Time{} },
		"gap in paid period":  func(i *stripe.Invoice, p *PaidPeriod) { p.End = p.End.Add(-time.Hour) },
		"other subscription":  func(i *stripe.Invoice, p *PaidPeriod) { p.SubscriptionID = "sub_other" },
		"quantity change":     func(i *stripe.Invoice, p *PaidPeriod) { p.Quantity++ },
		"incorrect remainder": func(i *stripe.Invoice, p *PaidPeriod) { i.AmountRemaining++ },
		"proration": func(i *stripe.Invoice, p *PaidPeriod) {
			i.Lines.Data[0].Parent.SubscriptionItemDetails.Proration = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			i, p, b, prior, now := fixture()
			change(i, &prior)
			if _, err := VerifyFailedRenewal(i, p, b, prior, now); err == nil {
				t.Fatal("ineligible failure accepted")
			}
		})
	}
}
