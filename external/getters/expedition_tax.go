package getters

import (
	"encoding/json"
	"time"

	"btcpp-web/internal/types"
)

// An organizer policy record, not a Stripe calculation or a customer's location.
// Keep the classification with the order without collecting an unnecessary address.
const expeditionTaxProvider = "conference_ticket"

func ExpeditionConferenceTaxQuote(expires time.Time) TaxQuoteInput {
	raw, _ := json.Marshal(map[string]any{"basis": "conference-ticket-default", "classification": types.StripeTaxCodeNontaxable, "sales_tax_amount_cents": 0})
	return TaxQuoteInput{Provider: expeditionTaxProvider, RawResponse: string(raw), ExpiresAt: &expires}
}
