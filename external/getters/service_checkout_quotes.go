package getters

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ServiceCheckoutQuote struct {
	ID    string
	Quote ServiceQuote
	Tax   TaxQuoteInput
}

// SaveAlertsCheckoutQuote accepts only trusted output from the account-aware
// pricing coordinator. Storage does not create an order, contact a provider, or
// change entitlements. Account state is checked again on confirmation.
func SaveAlertsCheckoutQuote(app *config.AppContext, personID string, quote ServiceQuote, tax TaxQuoteInput) (*ServiceCheckoutQuote, error) {
	revision, err := hex.DecodeString(quote.AccountRevision)
	if quote.LegalAcceptance != nil || err != nil || len(revision) != 32 || quote.ProductCode != "alerts.repository-slots" || strings.TrimSpace(personID) == "" ||
		quote.PeriodStart == nil || quote.PeriodEnd == nil || !quote.PeriodEnd.After(*quote.PeriodStart) || quote.ExpiresAt.After(*quote.PeriodEnd) ||
		!quote.ExpiresAt.After(time.Now()) || quote.Quantity < 1 || quote.Quantity > 2147483647 || quote.UnitCents < 1 || quote.UnitCents > 2147483647 ||
		quote.TotalCents < 1 || quote.TotalCents > 2147483647 || quote.Quantity*quote.UnitCents != quote.TotalCents || quote.TaxCents < 0 || quote.TaxCents > quote.TotalCents {
		return nil, ErrServiceQuote
	}
	if err = validateServiceTaxQuote(quote, tax); err != nil {
		return nil, err
	}
	tax = alertsTaxWithoutAddress(tax)
	rawQuote, err := json.Marshal(quote)
	if err != nil {
		return nil, err
	}
	rawTax, err := json.Marshal(tax)
	if err != nil {
		return nil, err
	}
	result := &ServiceCheckoutQuote{Quote: quote, Tax: tax}
	err = app.DB.QueryRow(app.DatabaseContext(), `INSERT INTO service_checkout_quotes(person_id,quote_snapshot,tax_snapshot,expires_at)
 VALUES($1,$2,$3,$4) RETURNING id::text`, personID, rawQuote, rawTax, quote.ExpiresAt).Scan(&result.ID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ServiceCheckoutQuoteForPerson includes expired quotes so the confirmation page
// can show their original terms and offer a new quote. Ownership is always checked.
func ServiceCheckoutQuoteForPerson(app *config.AppContext, personID, id string) (*ServiceCheckoutQuote, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, pgx.ErrNoRows
	}
	var rawQuote, rawTax []byte
	result := &ServiceCheckoutQuote{ID: parsed.String()}
	err = app.DB.QueryRow(app.DatabaseContext(), `SELECT quote_snapshot,tax_snapshot FROM service_checkout_quotes WHERE id=$1 AND person_id=$2`, parsed.String(), personID).Scan(&rawQuote, &rawTax)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(rawQuote, &result.Quote); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(rawTax, &result.Tax); err != nil {
		return nil, err
	}
	result.Tax = alertsTaxWithoutAddress(result.Tax)
	return result, nil
}

// ConfirmServiceCheckoutQuote creates exactly one order from the stored quote.
// Expiry/revision checks apply to new orders; retries return the original order.
// It does not charge the buyer. The existing order checkout starts payment.
func ConfirmServiceCheckoutQuote(app *config.AppContext, personID, id string) (*ServicePurchase, error) {
	return ConfirmServiceCheckoutQuoteWithPolicy(app, personID, id, "")
}

func ConfirmServiceCheckoutQuoteWithPolicy(app *config.AppContext, personID, id, acceptedVersion string) (*ServicePurchase, error) {
	stored, err := ServiceCheckoutQuoteForPerson(app, personID, id)
	if err != nil {
		return nil, err
	}
	return CreateTaxedServicePurchaseWithPolicy(app, personID, "checkout-quote:"+stored.ID, stored.Quote, stored.Tax, acceptedVersion)
}

// ConfirmedServiceQuoteOrder returns only the account-owned order already created
// for this quote. Reopening its review page should resume that order, not ask the
// buyer to accept a newer policy for an unchanged purchase.
func ConfirmedServiceQuoteOrder(app *config.AppContext, personID, quoteID string) (string, error) {
	if _, err := uuid.Parse(quoteID); err != nil {
		return "", pgx.ErrNoRows
	}
	var publicID string
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT o.public_id FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id WHERE r.person_id=$1 AND r.request_key=$2`, personID, "checkout-quote:"+quoteID).Scan(&publicID)
	return publicID, err
}

// Store only the provider reference, verified amount and expiry. Never persist
// a provider's full tax response: it can echo the customer's billing address.
func alertsTaxWithoutAddress(tax TaxQuoteInput) TaxQuoteInput {
	return TaxQuoteInput{OrderID: tax.OrderID, Provider: tax.Provider, ProviderQuoteID: tax.ProviderQuoteID, SalesTaxAmountCents: tax.SalesTaxAmountCents, RawResponse: "{}", ExpiresAt: tax.ExpiresAt}
}
