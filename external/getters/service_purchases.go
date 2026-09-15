package getters

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

var ErrServiceQuote = errors.New("invalid or expired service quote")
var ErrServicePurchaseConflict = errors.New("purchase key already used for another quote")
var ErrServiceBuyer = errors.New("service buyer needs a verified account email")

// ServiceQuote is trusted server output, never a client-supplied price. Product
// adapters must resolve pricing, tax and access periods before calling this API.
// Amounts are USD cents; UnitCents and TotalCents include TaxCents.
type ServiceQuote struct {
	// Alerts account evidence captured by the server during quote preparation.
	BaseOrderID     string     `json:"base_order_id,omitempty"`
	BillingMonths   int        `json:"billing_months,omitempty"`
	AccountRevision string     `json:"account_revision,omitempty"`
	ProductCode     string     `json:"product_code"`
	ResourceID      string     `json:"resource_id"`
	Name            string     `json:"name"`
	Provider        string     `json:"provider"`
	Quantity        int64      `json:"quantity"`
	UnitCents       int64      `json:"unit_cents"`
	TotalCents      int64      `json:"total_cents"`
	TaxCents        int64      `json:"tax_cents"`
	PeriodStart     *time.Time `json:"period_start"`
	PeriodEnd       *time.Time `json:"period_end"`
	ExpiresAt       time.Time  `json:"expires_at"`
}

type ServicePurchase struct {
	OrderID  string
	PublicID string
}

// CreateServicePurchase atomically persists one pending order and its immutable
// quote. It does not contact a provider or grant access. Exact retries return the
// original order even after quote expiry; changed quotes require a new key.
func CreateServicePurchase(app *config.AppContext, personID, key string, quote ServiceQuote) (*ServicePurchase, error) {
	return createServicePurchase(app, personID, key, quote, nil)
}

// CreateTaxedServicePurchase atomically stores the trusted pricing snapshot and
// its provider tax evidence. Retrying a key cannot replace the tax calculation.
func CreateTaxedServicePurchase(app *config.AppContext, personID, key string, quote ServiceQuote, tax TaxQuoteInput) (*ServicePurchase, error) {
	if tax.Provider != "stripe" {
		return nil, ErrServiceQuote
	}
	if !strings.HasPrefix(tax.ProviderQuoteID, "taxcalc_") || int64(tax.SalesTaxAmountCents) != quote.TaxCents || tax.ExpiresAt == nil || quote.ExpiresAt.After(*tax.ExpiresAt) || strings.TrimSpace(tax.DestinationCountry) == "" || !json.Valid([]byte(tax.RawResponse)) {
		return nil, ErrServiceQuote
	}
	return createServicePurchase(app, personID, key, quote, &tax)
}

func createServicePurchase(app *config.AppContext, personID, key string, quote ServiceQuote, tax *TaxQuoteInput) (*ServicePurchase, error) {
	if len(key) < 16 || len(key) > 128 || strings.TrimSpace(personID) == "" {
		return nil, ErrServiceQuote
	}
	snapshot, err := json.Marshal(quote)
	if err != nil {
		return nil, ErrServiceQuote
	}
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Serialize requests per account, including concurrent attempts with one key.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+personID); err != nil {
		return nil, err
	}
	var purchase ServicePurchase
	var same bool
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.public_id,r.quote_snapshot=$3::jsonb
		FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id
		WHERE r.person_id=$1 AND r.request_key=$2`, personID, key, snapshot).Scan(&purchase.OrderID, &purchase.PublicID, &same)
	if err == nil {
		if !same {
			return nil, ErrServicePurchaseConflict
		}
		if tax != nil {
			var sameTax bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tax_quotes WHERE order_id=$1 AND sales_tax_provider=$2 AND provider_quote_id=$3 AND sales_tax_amount_cents=$4 AND destination_country=$5 AND destination_region=$6 AND destination_postal_code=$7 AND raw_tax_response=$8::jsonb AND expires_at=$9)`, purchase.OrderID, tax.Provider, tax.ProviderQuoteID, int64(tax.SalesTaxAmountCents), tax.DestinationCountry, tax.DestinationRegion, tax.DestinationPostalCode, tax.RawResponse, tax.ExpiresAt).Scan(&sameTax)
			if err != nil {
				return nil, err
			}
			if !sameTax {
				return nil, ErrServicePurchaseConflict
			}
		}
		return &purchase, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if quote.AccountRevision != "" {
		if quote.ProductCode != "alerts.repository-slots" {
			return nil, ErrServiceQuote
		}
		revision, err := alertsBillingRevision(ctx, tx, personID)
		if err != nil {
			return nil, err
		}
		if revision != quote.AccountRevision {
			return nil, ErrAlertsBillingChanged
		}
	}
	method := "service"
	switch quote.ProductCode {
	case "alerts.repository-slots", "insider.membership":
		if quote.PeriodStart == nil || quote.PeriodEnd == nil || !quote.PeriodEnd.After(*quote.PeriodStart) {
			return nil, ErrServiceQuote
		}
	case "insider.article":
		method = "digital"
		if strings.TrimSpace(quote.ResourceID) == "" || quote.Quantity != 1 || quote.PeriodStart != nil || quote.PeriodEnd != nil {
			return nil, ErrServiceQuote
		}
	default:
		return nil, ErrServiceQuote
	}
	if (quote.Provider != "stripe" && quote.Provider != "opennode") || strings.TrimSpace(quote.Name) == "" ||
		quote.Quantity < 1 || quote.Quantity > 2147483647 || quote.UnitCents < 1 || quote.UnitCents > 2147483647 ||
		quote.TotalCents < 1 || quote.TotalCents > 2147483647 || quote.Quantity*quote.UnitCents != quote.TotalCents ||
		quote.TaxCents < 0 || quote.TaxCents > quote.TotalCents || !quote.ExpiresAt.After(time.Now()) {
		return nil, ErrServiceQuote
	}
	var email string
	err = tx.QueryRow(ctx, `SELECT email::text FROM person_emails WHERE person_id=$1 AND verified_at IS NOT NULL
		ORDER BY is_primary DESC,email LIMIT 1 FOR SHARE`, personID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServiceBuyer
	}
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO shop_orders(public_id,buyer_email,buyer_person_id,checkout_kind,
		payment_provider,currency,subtotal_cents,sales_tax_amount_cents,total_cents,checkout_expires_at)
		VALUES(gen_random_uuid()::text,$1,$2,$3,$4,'USD',$5,$6,$7,$8) RETURNING id::text,public_id`,
		email, personID, method, quote.Provider, quote.TotalCents-quote.TaxCents, quote.TaxCents, quote.TotalCents, quote.ExpiresAt).Scan(&purchase.OrderID, &purchase.PublicID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO shop_order_items(order_id,quantity,unit_price_cents,tax_amount_cents,line_total_cents,
		product_name_snapshot,fulfillment_method,service_product_code,service_resource_id,service_period_start,service_period_end)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, purchase.OrderID, quote.Quantity, quote.UnitCents, quote.TaxCents,
		quote.TotalCents, quote.Name, method, quote.ProductCode, quote.ResourceID, quote.PeriodStart, quote.PeriodEnd)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_purchase_requests(person_id,request_key,order_id,quote_snapshot) VALUES($1,$2,$3,$4)`, personID, key, purchase.OrderID, snapshot)
	if err != nil {
		return nil, err
	}
	if tax != nil {
		_, err = tx.Exec(ctx, `INSERT INTO tax_quotes(order_id,sales_tax_provider,provider_quote_id,sales_tax_amount_cents,destination_country,destination_region,destination_postal_code,raw_tax_response,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`, purchase.OrderID, tax.Provider, tax.ProviderQuoteID, int64(tax.SalesTaxAmountCents), tax.DestinationCountry, tax.DestinationRegion, tax.DestinationPostalCode, tax.RawResponse, tax.ExpiresAt)
		if err != nil {
			return nil, err
		}
	}
	return &purchase, tx.Commit(ctx)
}
