package alertsstripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestRenewalLink(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		change    func(*stripe.Invoice)
		valid     bool
	}{
		{"open", "https://invoice.stripe.com/i/acct_test/token?x=1", nil, true},
		{"paid", "https://invoice.stripe.com/i/acct_test/token", func(i *stripe.Invoice) { i.Status = "paid" }, true},
		{"foreign host", "https://invoice.stripe.com.evil.test/i/token", nil, false},
		{"userinfo", "https://evil@invoice.stripe.com/i/token", nil, false},
		{"http", "http://invoice.stripe.com/i/token", nil, false},
		{"empty path", "https://invoice.stripe.com/i/", nil, false},
		{"foreign customer", "https://invoice.stripe.com/i/token", func(i *stripe.Invoice) { i.Customer.ID = "cus_other" }, false},
		{"wrong invoice", "https://invoice.stripe.com/i/token", func(i *stripe.Invoice) { i.ID = "in_other" }, false},
		{"void", "https://invoice.stripe.com/i/token", func(i *stripe.Invoice) { i.Status = "void" }, false},
		{"initial", "https://invoice.stripe.com/i/token", func(i *stripe.Invoice) { i.BillingReason = "subscription_create" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv, price, b := invoiceFixture(t)
			id := inv.ID
			inv.Status = "open"
			inv.HostedInvoiceURL = tc.url
			if tc.change != nil {
				tc.change(inv)
			}
			c := Client{Live: b.Live, Invoice: func(requested string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
				if requested != id {
					t.Fatal(requested)
				}
				return inv, nil
			}, Price: func(id string, p *stripe.PriceParams) (*stripe.Price, error) { return price, nil }}
			got, err := c.RenewalLink(context.Background(), id, b)
			if tc.valid {
				if err != nil || got != tc.url {
					t.Fatal(got, err)
				}
			} else if err == nil || got != "" {
				t.Fatal("unsafe invoice link accepted", got, err)
			}
		})
	}
}
