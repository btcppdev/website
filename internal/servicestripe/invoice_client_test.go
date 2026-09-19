package servicestripe

import (
	"btcpp-web/internal/servicebilling"
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestPaidInvoiceAuthenticatedLookup(t *testing.T) {
	for _, code := range []string{servicebilling.InsiderMonthlyProduct, servicebilling.InsiderAnnualProduct} {
		t.Run(code, func(t *testing.T) {
			inv, price, b := insiderInvoice(t, code)
			invoiceCalls, priceCalls := 0, 0
			expectedID := inv.ID
			c := InvoiceClient{Invoice: func(id string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
				invoiceCalls++
				if id != expectedID || p.Context == nil {
					t.Fatal("invoice lookup")
				}
				return inv, nil
			}, Price: func(id string, p *stripe.PriceParams) (*stripe.Price, error) {
				priceCalls++
				if id != b.PriceID || len(p.Expand) != 1 || *p.Expand[0] != "product" {
					t.Fatal("price lookup")
				}
				return price, nil
			}}
			if got, err := c.PaidInvoice(context.Background(), inv.ID, b); err != nil || got.TotalCents != inv.Total {
				t.Fatal(got, err)
			}
			if invoiceCalls != 1 || priceCalls != 1 {
				t.Fatal("provider evidence not fetched")
			}
			b.Live = true
			if _, err := c.PaidInvoice(context.Background(), inv.ID, b); err == nil {
				t.Fatal("mode mismatch")
			}
			if invoiceCalls != 1 {
				t.Fatal("mode mismatch reached provider")
			}
			b.Live = false
			wanted := inv.ID
			inv.ID = "in_other"
			if _, err := c.PaidInvoice(context.Background(), wanted, b); err == nil {
				t.Fatal("different invoice accepted")
			}
			if priceCalls != 1 {
				t.Fatal("mismatched invoice reached price lookup")
			}
		})
	}
}
