package handlers

import (
	"btcpp-web/internal/servicebilling"
	"context"
	"errors"
	"fmt"
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
				if *p.Currency != "usd" || *p.CustomerDetails.AddressSource != "billing" || *p.CustomerDetails.Address.Country != "US" || len(p.LineItems) != 1 || *p.LineItems[0].Amount != 4800 || *p.LineItems[0].Quantity != 2 || *p.LineItems[0].TaxBehavior != "inclusive" || *p.LineItems[0].TaxCode != "txcd_10103000" || p.ShippingCost != nil {
					t.Fatal("incorrect inclusive calculation request")
				}
				if _, ok := p.Context.Deadline(); !ok {
					t.Fatal("missing request deadline")
				}
				tax := int64(200)
				if scenario == "zero-tax" {
					tax = 0
				}
				line := &stripe.TaxCalculationLineItem{Amount: 4800, AmountTax: tax, Quantity: 2, Reference: "alerts.repository-slots", TaxBehavior: "inclusive", TaxCode: "txcd_10103000"}
				c := &stripe.TaxCalculation{ID: "taxcalc_local", Currency: "usd", AmountTotal: 4800, TaxAmountInclusive: tax, ExpiresAt: start.Add(time.Hour).Unix(), LineItems: &stripe.TaxCalculationLineItemList{Data: []*stripe.TaxCalculationLineItem{line}}}
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
			if err != nil || quote.TotalCents != 4800 || quote.UnitCents != 2400 || quote.TaxCents != int64(tax.SalesTaxAmountCents) || !quote.PeriodEnd.Equal(start.AddDate(1, 0, 0)) || quote.ExpiresAt.After(start.Add(11*time.Minute)) || tax.ProviderQuoteID != "taxcalc_local" {
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

func TestAlertsProratedTaxQuote(t *testing.T) {
	address := &types.ShopAddress{Line1: "101 Main St", City: "Austin", Country: "US"}
	for _, term := range []struct {
		provider string
		months   int
	}{{"stripe", 1}, {"opennode", 12}, {"opennode", 24}, {"opennode", 120}} {
		t.Run(fmt.Sprintf("%s-%d", term.provider, term.months), func(t *testing.T) {
			start := time.Now().UTC()
			full, err := servicebilling.AlertsTerm(term.provider, 2, term.months, start)
			if err != nil {
				t.Fatal(err)
			}
			effective := start.Add(full.End.Sub(start) / 2)
			want := int64(term.months) * 200 // two slots, each half the full price
			quote, tax, err := alertsIncreaseTaxQuote(context.Background(), term.provider, 2, term.months, start, full.End, effective, address, func(p *stripe.TaxCalculationParams) (*stripe.TaxCalculation, error) {
				if *p.LineItems[0].Amount != want || *p.LineItems[0].Quantity != 2 || *p.LineItems[0].TaxBehavior != "inclusive" {
					t.Fatal("wrong prorated tax request")
				}
				return &stripe.TaxCalculation{ID: "taxcalc_increase", Currency: "usd", AmountTotal: want, TaxAmountInclusive: 10, ExpiresAt: time.Now().Add(time.Hour).Unix(), LineItems: &stripe.TaxCalculationLineItemList{Data: []*stripe.TaxCalculationLineItem{{Amount: want, AmountTax: 10, Quantity: 2, Reference: "alerts.repository-slots", TaxBehavior: "inclusive", TaxCode: servicebilling.AlertsStripeTaxCode}}}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if quote.TotalCents != want || quote.UnitCents*2 != want || quote.TaxCents != 10 || tax.SalesTaxAmountCents != 10 || !quote.PeriodStart.Equal(effective) || !quote.PeriodEnd.Equal(full.End) {
				t.Fatalf("incorrect increase quote: %+v", quote)
			}
			called := false
			calculate := func(*stripe.TaxCalculationParams) (*stripe.TaxCalculation, error) { called = true; return nil, nil }
			_, _, err = alertsIncreaseTaxQuote(context.Background(), term.provider, 2, term.months, start, full.End, full.End.Add(-time.Nanosecond), address, calculate)
			if !errors.Is(err, ErrAlertsZeroCharge) || called {
				t.Fatal("zero-price change reached payment provider", err)
			}
			_, _, err = alertsIncreaseTaxQuote(context.Background(), term.provider, 2, term.months, start, full.End.Add(time.Hour), effective, address, calculate)
			if !errors.Is(err, servicebilling.ErrAlertsPrice) || called {
				t.Fatal("changed paid-through date accepted", err)
			}
		})
	}
}

func TestAlertsTaxQuoteCannotOutlivePaidPeriod(t *testing.T) {
	now := time.Now().UTC()
	price := servicebilling.AlertsPrice{Slots: 1, UnitCents: 1, TotalCents: 1, Start: now, End: now.Add(time.Minute)}
	quote, _, err := alertsPriceTaxQuote(context.Background(), "stripe", price, &types.ShopAddress{Line1: "101 Main St", City: "Austin", Country: "US"}, func(*stripe.TaxCalculationParams) (*stripe.TaxCalculation, error) {
		return &stripe.TaxCalculation{ID: "taxcalc_short", Currency: "usd", AmountTotal: 1, ExpiresAt: now.Add(time.Hour).Unix(), LineItems: &stripe.TaxCalculationLineItemList{Data: []*stripe.TaxCalculationLineItem{{Amount: 1, Quantity: 1, Reference: "alerts.repository-slots", TaxBehavior: "inclusive", TaxCode: servicebilling.AlertsStripeTaxCode}}}}, nil
	})
	if err != nil || !quote.ExpiresAt.Equal(price.End) {
		t.Fatal("quote can be paid after its access period", quote, err)
	}
}
