package getters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

var ErrAlertsBillingChanged = errors.New("Alerts billing changed; request a new quote")

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
  'grant',e.id,'quantity',e.quantity,'starts',extract(epoch from e.starts_at),'ends',extract(epoch from e.ends_at),'revoked',extract(epoch from e.revoked_at)
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
