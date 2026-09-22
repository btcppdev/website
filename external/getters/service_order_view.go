package getters

import (
	"context"
	"encoding/json"
	"time"

	"btcpp-web/internal/config"
)

type ServiceOrderView struct {
	ID              string
	PublicID        string
	Status          string
	Currency        string
	TotalCents      int64
	TaxCents        int64
	RefundedCents   int64
	CreatedAt       time.Time
	PaymentVerified bool
	AccessState     string
	Quote           ServiceQuote
}

// ServiceOrderForPerson authorizes in the query, using the immutable purchase's
// account binding. Neither knowledge of an order URL nor matching email is enough.
func ServiceOrderForPerson(ctx context.Context, app *config.AppContext, personID, publicID string) (*ServiceOrderView, error) {
	var order ServiceOrderView
	var raw []byte
	err := app.DB.QueryRow(ctx, `SELECT o.id::text,o.public_id,o.status,o.currency,o.total_cents,o.sales_tax_amount_cents,o.created_at,r.quote_snapshot,
 (SELECT coalesce(sum(f.amount_cents),0) FROM service_payment_refunds f JOIN service_payment_attempts a ON a.id=f.attempt_id WHERE a.purchase_id=r.id),
		EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id AND a.settled_at IS NOT NULL),
		CASE WHEN e.id IS NULL THEN 'pending'
		WHEN e.revoked_at IS NOT NULL OR o.status<>'paid' THEN 'inactive'
		WHEN e.ends_at IS NOT NULL AND e.ends_at<=now() THEN 'expired'
		WHEN e.starts_at IS NOT NULL AND e.starts_at>now() THEN 'scheduled'
		ELSE 'active' END
		FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id
		LEFT JOIN service_entitlements e ON e.purchase_id=r.id
		WHERE r.person_id=$1 AND o.public_id=$2`, personID, publicID).Scan(&order.ID, &order.PublicID, &order.Status, &order.Currency, &order.TotalCents, &order.TaxCents, &order.CreatedAt, &raw, &order.RefundedCents, &order.PaymentVerified, &order.AccessState)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &order.Quote); err != nil {
		return nil, err
	}
	return &order, nil
}
