package getters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
	stripeTaxTransaction "github.com/stripe/stripe-go/v86/tax/transaction"
)

// FinalizeServiceTax records a settled service order in Stripe Tax. This does
// not charge the customer. Failed recording must be retried independently of
// access activation. Existing purchases without a tax calculation are skipped.
func FinalizeServiceTax(ctx context.Context, app *config.AppContext, orderID string) error {
	client := stripeTaxTransaction.Client{B: stripe.GetBackend(stripe.APIBackend), Key: app.Env.StripeKey}
	return finalizeServiceTax(ctx, app, orderID, func(params *stripe.TaxTransactionCreateFromCalculationParams) (*stripe.TaxTransaction, error) {
		if strings.TrimSpace(app.Env.StripeKey) == "" {
			return nil, fmt.Errorf("Stripe Tax is not configured")
		}
		return client.CreateFromCalculation(params)
	})
}

func finalizeServiceTax(ctx context.Context, app *config.AppContext, orderID string, create func(*stripe.TaxTransactionCreateFromCalculationParams) (*stripe.TaxTransaction, error)) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var paidAt time.Time
	err = tx.QueryRow(ctx, `SELECT r.quote_snapshot,a.settled_at FROM shop_orders o
		JOIN service_purchase_requests r ON r.order_id=o.id JOIN service_payment_attempts a ON a.purchase_id=r.id
		WHERE o.id=$1 AND o.status IN ('paid','partially_refunded','refunded') AND a.settled_at IS NOT NULL
		AND a.provider=o.payment_provider AND a.provider_reference=o.payment_provider_id FOR UPDATE OF o`, orderID).Scan(&raw, &paidAt)
	if err != nil {
		return err
	}
	var quote ServiceQuote
	if err = json.Unmarshal(raw, &quote); err != nil {
		return err
	}
	var recorded bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tax_transactions WHERE order_id=$1 AND provider='stripe' AND status='recorded')`, orderID).Scan(&recorded); err != nil {
		return err
	}
	if recorded {
		return tx.Commit(ctx)
	}
	var calculation string
	var taxAmount int64
	err = tx.QueryRow(ctx, `SELECT provider_quote_id,sales_tax_amount_cents FROM tax_quotes WHERE order_id=$1 AND sales_tax_provider='stripe' ORDER BY created_at DESC LIMIT 1`, orderID).Scan(&calculation, &taxAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if !strings.HasPrefix(calculation, "taxcalc_") || taxAmount != quote.TaxCents {
		return fmt.Errorf("service tax calculation requires review")
	}
	params := &stripe.TaxTransactionCreateFromCalculationParams{Calculation: stripe.String(calculation), Reference: stripe.String("service-order-" + orderID), PostedAt: stripe.Int64(paidAt.Unix())}
	params.Context = ctx
	params.SetIdempotencyKey("service-tax-transaction:" + orderID)
	params.AddExpand("line_items")
	result, err := create(params)
	if err != nil {
		return fmt.Errorf("record service tax: %w", err)
	}
	if result == nil || !strings.HasPrefix(result.ID, "tax_") || result.Reference != *params.Reference || result.Currency != "usd" || result.Type != "transaction" || result.LineItems == nil || len(result.LineItems.Data) != 1 {
		return fmt.Errorf("service tax transaction mismatch")
	}
	line := result.LineItems.Data[0]
	if line == nil || line.Amount != quote.TotalCents || line.AmountTax != quote.TaxCents || line.Quantity != quote.Quantity || line.Reference != quote.ProductCode || line.TaxBehavior != "inclusive" {
		return fmt.Errorf("service tax transaction line mismatch")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO tax_transactions(order_id,provider,provider_transaction_id,sales_tax_amount_cents,status,raw_response) VALUES($1,'stripe',$2,$3,'recorded',$4::jsonb)`, orderID, result.ID, quote.TaxCents, string(encoded))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
