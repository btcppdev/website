package getters

import (
	"encoding/json"
	"errors"
	"time"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

var ErrServiceFulfillment = errors.New("service purchase requires review before fulfillment")

type ServiceEntitlement struct {
	ID          string     `json:"id"`
	ProductCode string     `json:"productCode"`
	ResourceID  string     `json:"resourceId"`
	Quantity    int64      `json:"quantity"`
	StartsAt    *time.Time `json:"startsAt"`
	EndsAt      *time.Time `json:"endsAt"`
}

// FulfillServicePurchase is an internal worker operation. It grants exactly the
// immutable purchased access after settlement, never accepts client grant fields,
// and is replay-safe. Existing revoked grants are never reactivated by retries.
func FulfillServicePurchase(app *config.AppContext, orderID string) (string, error) {
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var purchaseID, personID, status string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT r.id::text,coalesce(r.person_id::text,''),r.quote_snapshot,o.status
		FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id WHERE o.id=$1 FOR UPDATE OF o`, orderID).Scan(&purchaseID, &personID, &raw, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrServiceFulfillment
	}
	if err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM service_entitlements WHERE purchase_id=$1`, purchaseID).Scan(&id)
	if err == nil {
		return id, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if personID == "" || status != "paid" {
		return "", ErrServiceFulfillment
	}
	var quote ServiceQuote
	if err = json.Unmarshal(raw, &quote); err != nil {
		return "", err
	}
	if quote.PeriodEnd != nil && !quote.PeriodEnd.After(time.Now()) {
		return "", ErrServiceFulfillment
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM shop_order_items WHERE order_id=$1 FOR UPDATE`, orderID); err != nil {
		return "", err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id
		AND a.settled_at IS NOT NULL AND a.provider=o.payment_provider AND a.provider_reference=o.payment_provider_id)
		AND o.currency='USD' AND o.total_cents=$2 AND o.sales_tax_amount_cents=$3 AND o.subtotal_cents=$2-$3
		AND o.shipping_amount_cents=0 AND o.discount_amount_cents=0 AND o.import_duty_amount_cents=0 AND o.import_tax_amount_cents=0
		AND NOT EXISTS(SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id
		  AND (i.status<>'pending' OR i.fulfilled_quantity<>0 OR i.refunded_quantity<>0))
		AND `+serviceItemsMatchQuoteSQL+`
		FROM shop_orders o JOIN service_purchase_requests r ON r.order_id=o.id WHERE o.id=$1`, orderID, quote.TotalCents, quote.TaxCents).Scan(&valid)
	if err != nil {
		return "", err
	}
	if !valid {
		return "", ErrServiceFulfillment
	}
	err = tx.QueryRow(ctx, `INSERT INTO service_entitlements(purchase_id,person_id,product_code,resource_id,quantity,starts_at,ends_at)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, purchaseID, personID, quote.ProductCode, quote.ResourceID, quote.Quantity, quote.PeriodStart, quote.PeriodEnd).Scan(&id)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `UPDATE shop_order_items SET fulfilled_quantity=quantity,status='fulfilled' WHERE order_id=$1`, orderID)
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

// ActiveServiceEntitlements is account-scoped. Refund state suppresses access
// immediately, even before a separate refund worker records explicit revocation.
// Partial refunds are withheld pending product-specific quantity reconciliation.
func ActiveServiceEntitlements(app *config.AppContext, personID string, at time.Time) ([]ServiceEntitlement, error) {
	rows, err := app.DB.Query(app.DatabaseContext(), `SELECT e.id::text,e.product_code,e.resource_id,e.quantity,e.starts_at,e.ends_at
		FROM service_entitlements e JOIN service_purchase_requests r ON r.id=e.purchase_id JOIN shop_orders o ON o.id=r.order_id
		WHERE e.person_id=$1 AND r.person_id=e.person_id AND e.revoked_at IS NULL AND o.status='paid'
		AND (e.starts_at IS NULL OR e.starts_at<=$2) AND (e.ends_at IS NULL OR e.ends_at>$2)
		ORDER BY e.created_at,e.id`, personID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceEntitlement{}
	for rows.Next() {
		var e ServiceEntitlement
		if err = rows.Scan(&e.ID, &e.ProductCode, &e.ResourceID, &e.Quantity, &e.StartsAt, &e.EndsAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
