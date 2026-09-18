package getters

import (
	"encoding/json"
	"errors"
	"strings"

	"btcpp-web/internal/config"

	"github.com/jackc/pgx/v5"
)

// CompleteExternalServiceFulfillment marks a paid service item delivered only
// after its product owner has durably accepted the fulfillment event. It does
// not create a btcpp.dev entitlement: Insider remains the access authority and
// projects only the reader's coarse tier back to btcpp.dev.
func CompleteExternalServiceFulfillment(app *config.AppContext, orderID string) error {
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var purchaseID, status string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT r.id::text,r.quote_snapshot,o.status
		FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id
		WHERE o.id=$1 FOR UPDATE OF o`, orderID).Scan(&purchaseID, &raw, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServiceFulfillment
	}
	if err != nil {
		return err
	}
	if status != "paid" {
		return ErrServiceFulfillment
	}
	var quote ServiceQuote
	if err := json.Unmarshal(raw, &quote); err != nil {
		return err
	}
	if !strings.HasPrefix(quote.ProductCode, "insider.") {
		return ErrServiceFulfillment
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM shop_order_items WHERE order_id=$1 FOR UPDATE`, orderID); err != nil {
		return err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id
		AND a.settled_at IS NOT NULL AND a.provider=o.payment_provider AND a.provider_reference=o.payment_provider_id)
		AND o.currency='USD' AND o.total_cents=$2 AND o.sales_tax_amount_cents=$3 AND o.subtotal_cents=$2-$3
		AND o.shipping_amount_cents=0 AND o.discount_amount_cents=0 AND o.import_duty_amount_cents=0 AND o.import_tax_amount_cents=0
		AND NOT EXISTS(SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id
		  AND (i.status NOT IN ('pending','fulfilled') OR i.refunded_quantity<>0
		  OR (i.status='pending' AND i.fulfilled_quantity<>0)
		  OR (i.status='fulfilled' AND i.fulfilled_quantity<>i.quantity)))
		AND `+serviceItemsMatchQuoteSQL+`
		FROM shop_orders o JOIN service_purchase_requests r ON r.order_id=o.id WHERE o.id=$1`, orderID, quote.TotalCents, quote.TaxCents).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrServiceFulfillment
	}
	_, err = tx.Exec(ctx, `UPDATE shop_order_items SET fulfilled_quantity=quantity,status='fulfilled'
		WHERE order_id=$1 AND status='pending'`, orderID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
