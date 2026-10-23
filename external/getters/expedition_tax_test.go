package getters

import (
	"btcpp-web/internal/expedition"
	"testing"
	"time"
)

func TestConferencePassageTaxEvidence(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	quote := ServiceQuote{ProductCode: expedition.ProductCode, TotalCents: 334000, ExpiresAt: expiry}
	tax := ExpeditionConferenceTaxQuote(expiry)
	if err := validateServiceTaxQuote(quote, tax); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ServiceQuote, *TaxQuoteInput){
		"other product":              func(q *ServiceQuote, _ *TaxQuoteInput) { q.ProductCode = "alerts.repository-slots" },
		"tax charged":                func(q *ServiceQuote, t *TaxQuoteInput) { q.TaxCents = 1; t.SalesTaxAmountCents = 1 },
		"customer location invented": func(_ *ServiceQuote, t *TaxQuoteInput) { t.DestinationCountry = "US" },
		"fake external quote":        func(_ *ServiceQuote, t *TaxQuoteInput) { t.ProviderQuoteID = "taxcalc_fake" },
		"altered policy":             func(_ *ServiceQuote, t *TaxQuoteInput) { t.RawResponse = `{}` },
		"missing expiry":             func(_ *ServiceQuote, t *TaxQuoteInput) { t.ExpiresAt = nil },
	} {
		t.Run(name, func(t *testing.T) {
			q, tx := quote, tax
			change(&q, &tx)
			if validateServiceTaxQuote(q, tx) == nil {
				t.Fatal("invalid conference tax evidence accepted")
			}
		})
	}
}
