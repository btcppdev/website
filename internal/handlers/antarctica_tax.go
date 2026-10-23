package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"fmt"
	"github.com/stripe/stripe-go/v86"
	stripeTaxCalculation "github.com/stripe/stripe-go/v86/tax/calculation"
	"strings"
	"time"
)

func expeditionTaxQuote(ctx context.Context, app *config.AppContext, charge expedition.Charge, address *types.ShopAddress) (*getters.TaxQuoteInput, error) {
	client := stripeTaxCalculation.Client{B: stripe.GetBackend(stripe.APIBackend), Key: app.Env.StripeKey}
	return calculateExpeditionTax(ctx, app, charge, address, client.New)
}
func calculateExpeditionTax(ctx context.Context, app *config.AppContext, charge expedition.Charge, address *types.ShopAddress, calculate func(*stripe.TaxCalculationParams) (*stripe.TaxCalculation, error)) (*getters.TaxQuoteInput, error) {
	taxCode := app.Env.AntarcticaStripeTaxCode
	if address == nil || strings.TrimSpace(address.Country) == "" || strings.TrimSpace(address.Line1) == "" || strings.TrimSpace(address.City) == "" {
		return nil, fmt.Errorf("billing address is required")
	}
	params := &stripe.TaxCalculationParams{
		Currency: stripe.String("usd"),
		CustomerDetails: &stripe.TaxCalculationCustomerDetailsParams{AddressSource: stripe.String("billing"), Address: &stripe.AddressParams{
			Line1: stripe.String(strings.TrimSpace(address.Line1)), Line2: stripe.String(strings.TrimSpace(address.Line2)), City: stripe.String(strings.TrimSpace(address.City)), State: stripe.String(strings.TrimSpace(address.Region)), Country: stripe.String(strings.ToUpper(strings.TrimSpace(address.Country))),
		}},
		LineItems: []*stripe.TaxCalculationLineItemParams{{Amount: stripe.Int64(charge.TotalCents), Quantity: stripe.Int64(int64(1)), Reference: stripe.String(expedition.ProductCode), TaxBehavior: stripe.String("inclusive"), TaxCode: stripe.String(taxCode)}},
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
		return nil, fmt.Errorf("calculate expedition tax: %w", err)
	}
	now := time.Now().UTC()
	if calculation == nil || !strings.HasPrefix(calculation.ID, "taxcalc_") || calculation.Currency != "usd" || calculation.AmountTotal != charge.TotalCents || calculation.TaxAmountExclusive != 0 || calculation.TaxAmountInclusive < 0 || calculation.TaxAmountInclusive > charge.TotalCents || calculation.ExpiresAt <= now.Unix() || calculation.LineItems == nil || len(calculation.LineItems.Data) != 1 {
		return nil, fmt.Errorf("tax calculation does not match the inclusive expedition price")
	}
	line := calculation.LineItems.Data[0]
	if line == nil || line.Amount != charge.TotalCents || line.AmountTax != calculation.TaxAmountInclusive || line.Quantity != int64(1) || line.Reference != expedition.ProductCode || line.TaxBehavior != "inclusive" || line.TaxCode != taxCode {
		return nil, fmt.Errorf("tax line does not match the expedition purchase")
	}
	expiry := time.Unix(calculation.ExpiresAt, 0).UTC()
	if cap := now.Add(45 * time.Minute); expiry.After(cap) {
		expiry = cap
	}
	if expiry.After(charge.ExpiresAt) {
		expiry = charge.ExpiresAt
	}
	if !expiry.After(now) || (charge.Provider == expedition.Stripe && expiry.Before(now.Add(31*time.Minute))) {
		return nil, expedition.ErrExpired
	}
	raw, err := json.Marshal(calculation)
	if err != nil {
		return nil, err
	}
	tax := &getters.TaxQuoteInput{Provider: types.TaxProviderStripe, ProviderQuoteID: calculation.ID, SalesTaxAmountCents: uint(calculation.TaxAmountInclusive), DestinationCountry: *params.CustomerDetails.Address.Country, DestinationRegion: strings.TrimSpace(address.Region), DestinationPostalCode: strings.TrimSpace(address.PostalCode), RawResponse: string(raw), ExpiresAt: &expiry}
	return tax, nil
}
