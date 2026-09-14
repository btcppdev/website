package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/types"
	"github.com/stripe/stripe-go/v86"
	stripeTaxCalculation "github.com/stripe/stripe-go/v86/tax/calculation"
)

// AlertsTermTaxQuote is internal checkout preparation, not an HTTP endpoint.
// Slots/term must come from the account's billing change. Prices and the confirmed
// personal-use SaaS tax code are server-owned; no caller supplies tax cents.
func AlertsTermTaxQuote(ctx context.Context, provider string, slots int64, months int, start time.Time, address *types.ShopAddress) (*getters.ServiceQuote, *getters.TaxQuoteInput, error) {
	return alertsTermTaxQuote(ctx, provider, slots, months, start, address, stripeTaxCalculation.New)
}

func alertsTermTaxQuote(ctx context.Context, provider string, slots int64, months int, start time.Time, address *types.ShopAddress, calculate func(*stripe.TaxCalculationParams) (*stripe.TaxCalculation, error)) (*getters.ServiceQuote, *getters.TaxQuoteInput, error) {
	price, err := servicebilling.AlertsTerm(provider, slots, months, start)
	if err != nil {
		return nil, nil, err
	}
	taxCode := servicebilling.AlertsStripeTaxCode
	if address == nil || strings.TrimSpace(address.Country) == "" || strings.TrimSpace(address.Line1) == "" || strings.TrimSpace(address.City) == "" {
		return nil, nil, fmt.Errorf("billing address is required")
	}
	params := &stripe.TaxCalculationParams{
		Currency: stripe.String("usd"),
		CustomerDetails: &stripe.TaxCalculationCustomerDetailsParams{AddressSource: stripe.String("billing"), Address: &stripe.AddressParams{
			Line1: stripe.String(strings.TrimSpace(address.Line1)), Line2: stripe.String(strings.TrimSpace(address.Line2)), City: stripe.String(strings.TrimSpace(address.City)), State: stripe.String(strings.TrimSpace(address.Region)), Country: stripe.String(strings.ToUpper(strings.TrimSpace(address.Country))),
		}},
		LineItems: []*stripe.TaxCalculationLineItemParams{{Amount: stripe.Int64(price.TotalCents), Quantity: stripe.Int64(price.Slots), Reference: stripe.String("alerts.repository-slots"), TaxBehavior: stripe.String("inclusive"), TaxCode: stripe.String(taxCode)}},
	}
	if postal := strings.TrimSpace(address.PostalCode); postal != "" {
		params.CustomerDetails.Address.PostalCode = stripe.String(postal)
	}
	params.AddExpand("line_items")
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	params.Context = requestCtx
	calculation, err := calculate(params)
	if err != nil {
		return nil, nil, fmt.Errorf("calculate Alerts tax: %w", err)
	}
	now := time.Now().UTC()
	if calculation == nil || !strings.HasPrefix(calculation.ID, "taxcalc_") || calculation.Currency != "usd" || calculation.AmountTotal != price.TotalCents || calculation.TaxAmountExclusive != 0 || calculation.TaxAmountInclusive < 0 || calculation.TaxAmountInclusive > price.TotalCents || calculation.ExpiresAt <= now.Unix() || calculation.LineItems == nil || len(calculation.LineItems.Data) != 1 {
		return nil, nil, fmt.Errorf("tax calculation does not match the inclusive Alerts price")
	}
	line := calculation.LineItems.Data[0]
	if line == nil || line.Amount != price.TotalCents || line.AmountTax != calculation.TaxAmountInclusive || line.Quantity != price.Slots || line.Reference != "alerts.repository-slots" || line.TaxBehavior != "inclusive" || line.TaxCode != taxCode {
		return nil, nil, fmt.Errorf("tax line does not match the Alerts purchase")
	}
	expiry := time.Unix(calculation.ExpiresAt, 0).UTC()
	if cap := now.Add(10 * time.Minute); expiry.After(cap) {
		expiry = cap
	}
	raw, err := json.Marshal(calculation)
	if err != nil {
		return nil, nil, err
	}
	quote := &getters.ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Bitcoin++ Alerts additional projects", Provider: provider, Quantity: price.Slots, UnitCents: price.UnitCents, TotalCents: price.TotalCents, TaxCents: calculation.TaxAmountInclusive, PeriodStart: &price.Start, PeriodEnd: &price.End, ExpiresAt: expiry}
	tax := &getters.TaxQuoteInput{Provider: types.TaxProviderStripe, ProviderQuoteID: calculation.ID, SalesTaxAmountCents: uint(calculation.TaxAmountInclusive), DestinationCountry: *params.CustomerDetails.Address.Country, DestinationRegion: strings.TrimSpace(address.Region), DestinationPostalCode: strings.TrimSpace(address.PostalCode), RawResponse: string(raw), ExpiresAt: &expiry}
	return quote, tax, nil
}
