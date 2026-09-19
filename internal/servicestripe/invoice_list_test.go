package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestAlertsStripePaidInvoiceDiscovery(t *testing.T) {
	inv, _, binding := invoiceFixture(t)
	page := &stripe.InvoiceList{Data: []*stripe.Invoice{inv}}
	c := InvoiceClient{ListInvoices: func(p *stripe.InvoiceListParams) (*stripe.InvoiceList, error) {
		if *p.Subscription != binding.SubscriptionID || *p.Customer != binding.CustomerID || *p.Status != "paid" || *p.Limit != 100 || !p.Single || p.Context == nil {
			t.Fatal("unscoped invoice request", p)
		}
		return page, nil
	}}
	ids, next, err := c.PaidInvoiceIDs(context.Background(), binding, "")
	if err != nil || len(ids) != 1 || ids[0] != inv.ID || next != "" {
		t.Fatal(ids, next, err)
	}
	page.HasMore = true
	_, next, err = c.PaidInvoiceIDs(context.Background(), binding, "")
	if err != nil || next != inv.ID {
		t.Fatal(next, err)
	}
	if _, _, err = c.PaidInvoiceIDs(context.Background(), binding, inv.ID); err == nil {
		t.Fatal("repeated cursor accepted")
	}
	page.Data = nil
	if _, _, err = c.PaidInvoiceIDs(context.Background(), binding, ""); err == nil {
		t.Fatal("empty continuing page accepted")
	}
	page.HasMore = false
	page.Data = []*stripe.Invoice{inv, inv}
	if _, _, err = c.PaidInvoiceIDs(context.Background(), binding, ""); err == nil {
		t.Fatal("duplicate invoice accepted")
	}
	page.Data = []*stripe.Invoice{inv}
	inv.Customer = &stripe.Customer{ID: "cus_other"}
	if _, _, err = c.PaidInvoiceIDs(context.Background(), binding, ""); err == nil {
		t.Fatal("wrong account accepted")
	}
}
