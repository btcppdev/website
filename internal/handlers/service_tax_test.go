package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/types"
	"github.com/stripe/stripe-go/v86"
)

func TestAlertsInclusiveTaxQuote(t *testing.T) {
	address := &types.ShopAddress{Line1: "101 Main St", City: "Austin", Country: "us", Region: "TX", PostalCode: "78701"}
	for _, scenario := range []string{"taxed", "zero-tax", "exclusive", "total", "currency", "expired", "wrong-line", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			start := time.Now().UTC()
			quote, tax, err := alertsTermTaxQuote(context.Background(), "opennode", 2, 12, start, address, func(p *stripe.TaxCalculationParams) (*stripe.TaxCalculation, error) {
				if *p.Currency != "usd" || *p.CustomerDetails.AddressSource != "billing" || *p.CustomerDetails.Address.Country != "US" || len(p.LineItems) != 1 || *p.LineItems[0].Amount != 2400 || *p.LineItems[0].Quantity != 2 || *p.LineItems[0].TaxBehavior != "inclusive" || *p.LineItems[0].TaxCode != "txcd_10103000" || p.ShippingCost != nil {
					t.Fatal("incorrect inclusive calculation request")
				}
				if _, ok := p.Context.Deadline(); !ok {
					t.Fatal("missing request deadline")
				}
				tax := int64(200)
				if scenario == "zero-tax" {
					tax = 0
				}
				line := &stripe.TaxCalculationLineItem{Amount: 2400, AmountTax: tax, Quantity: 2, Reference: "alerts.repository-slots", TaxBehavior: "inclusive", TaxCode: "txcd_10103000"}
				c := &stripe.TaxCalculation{ID: "taxcalc_local", Currency: "usd", AmountTotal: 2400, TaxAmountInclusive: tax, ExpiresAt: start.Add(time.Hour).Unix(), LineItems: &stripe.TaxCalculationLineItemList{Data: []*stripe.TaxCalculationLineItem{line}}}
				switch scenario {
				case "exclusive":
					c.TaxAmountExclusive = 200
				case "total":
					c.AmountTotal++
				case "currency":
					c.Currency = "eur"
				case "expired":
					c.ExpiresAt = start.Add(-time.Minute).Unix()
				case "wrong-line":
					line.Reference = "other"
				case "failure":
					return nil, errors.New("provider unavailable")
				}
				return c, nil
			})
			valid := scenario == "taxed" || scenario == "zero-tax"
			if !valid {
				if err == nil || quote != nil || tax != nil {
					t.Fatal("invalid response accepted")
				}
				return
			}
			if err != nil || quote.TotalCents != 2400 || quote.UnitCents != 1200 || quote.TaxCents != int64(tax.SalesTaxAmountCents) || !quote.PeriodEnd.Equal(start.AddDate(1, 0, 0)) || quote.ExpiresAt.After(start.Add(11*time.Minute)) || tax.ProviderQuoteID != "taxcalc_local" {
				t.Fatal("incorrect quote", quote, tax, err)
			}
		})
	}
}

func TestAlertsTaxRequiresConfiguredInputs(t *testing.T) {
	called := false
	_, _, err := alertsTermTaxQuote(context.Background(), "opennode", 1, 12, time.Now(), nil, func(*stripe.TaxCalculationParams) (*stripe.TaxCalculation, error) { called = true; return nil, nil })
	if err == nil || called {
		t.Fatal("missing tax configuration reached provider")
	}
}
