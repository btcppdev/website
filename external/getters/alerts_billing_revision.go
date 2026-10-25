package getters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

var ErrAlertsBillingChanged = errors.New("Alerts billing changed; request a new quote")

type AlertsBillingOrder struct {
	OrderID        string
	PublicID       string
	Status         string
	Quote          ServiceQuote
	Granted        bool
	Revoked        bool
	PaymentStarted bool
	RefundRetained bool
}

type AlertsBillingState struct {
	Revision string
	Orders   []AlertsBillingOrder
}

// LoadAlertsBillingState reads pricing evidence and its revision while holding
// the same locks used to confirm a purchase. No provider calls run under locks.
func LoadAlertsBillingState(app *config.AppContext, personID string) (*AlertsBillingState, error) {
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+personID); err != nil {
		return nil, err
	}
	revision, err := alertsBillingRevision(ctx, tx, personID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT o.id::text,o.public_id,o.status,r.quote_snapshot,e.id IS NOT NULL,e.revoked_at IS NOT NULL,
 EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id),
 (o.status IN ('refunded','partially_refunded') AND `+servicePurchaseAccessSQL+`)
 FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id
 LEFT JOIN service_entitlements e ON e.purchase_id=r.id AND e.person_id=r.person_id
 WHERE r.person_id=$1 AND r.quote_snapshot->>'product_code'='alerts.repository-slots' ORDER BY r.created_at,r.id`, personID)
	if err != nil {
		return nil, err
	}
	state := &AlertsBillingState{Revision: revision, Orders: []AlertsBillingOrder{}}
	for rows.Next() {
		var order AlertsBillingOrder
		var raw []byte
		if err = rows.Scan(&order.OrderID, &order.PublicID, &order.Status, &raw, &order.Granted, &order.Revoked, &order.PaymentStarted, &order.RefundRetained); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &order.Quote); err != nil {
			rows.Close()
			return nil, err
		}
		state.Orders = append(state.Orders, order)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return state, tx.Commit(ctx)
}

// AlertsBillingRevision binds checkout preparation to the account's purchase and
// entitlement evidence, including pending orders and scheduled or revoked access.
// The coordinator must attach this revision to its server-generated ServiceQuote.
// It does not authorize a quantity, choose a plan, or extend an access period.
func AlertsBillingRevision(app *config.AppContext, personID string) (string, error) {
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+personID); err != nil {
		return "", err
	}
	revision, err := alertsBillingRevision(ctx, tx, personID)
	if err != nil {
		return "", err
	}
	return revision, tx.Commit(ctx)
}

// Call with the same per-account purchase lock used by createServicePurchase.
// Order locks serialize fulfillment/refunds; entitlement locks serialize direct
// revocation. Lock before reading the snapshot so a committed change is visible.
func alertsBillingRevision(ctx context.Context, tx pgx.Tx, personID string) (string, error) {
	if _, err := tx.Exec(ctx, `SELECT o.id FROM shop_orders o JOIN service_purchase_requests r ON r.order_id=o.id WHERE r.person_id=$1 AND r.quote_snapshot->>'product_code'='alerts.repository-slots' ORDER BY o.id FOR SHARE OF o`, personID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `SELECT e.id FROM service_entitlements e WHERE e.person_id=$1 AND e.product_code='alerts.repository-slots' ORDER BY e.id FOR SHARE`, personID); err != nil {
		return "", err
	}
	var snapshot string
	err := tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object(
  'purchase',r.id,'quote',r.quote_snapshot,'order',o.id,'status',o.status,
  'grant',e.id,'quantity',e.quantity,'starts',extract(epoch from e.starts_at),'ends',extract(epoch from e.ends_at),'revoked',extract(epoch from e.revoked_at),
  'refunds',(SELECT coalesce(jsonb_agg(jsonb_build_object('provider',f.provider,'reference',f.refund_reference,'amount',f.amount_cents,'access',d.action) ORDER BY f.provider,f.refund_reference),'[]'::jsonb)
    FROM service_payment_attempts a JOIN service_payment_refunds f ON f.attempt_id=a.id
    LEFT JOIN service_refund_access_decisions d ON d.provider=f.provider AND d.refund_reference=f.refund_reference
    WHERE a.purchase_id=r.id)
 ) ORDER BY r.id),'[]'::jsonb)::text
 FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id
 LEFT JOIN service_entitlements e ON e.purchase_id=r.id AND e.person_id=r.person_id
 WHERE r.person_id=$1 AND r.quote_snapshot->>'product_code'='alerts.repository-slots'`, personID).Scan(&snapshot)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(personID + "\n" + snapshot))
	return hex.EncodeToString(digest[:]), nil
}
