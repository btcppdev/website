package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// A prepaid renewal is a new full term, starting after the account's existing
// prepaid period. Added slots share that boundary and count toward the renewal.
func AlertsPrepaidRenewal(ctx context.Context, app *config.AppContext, person, order string) (int64, time.Time, error) {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer tx.Rollback(ctx)
	n, end, err := alertsPrepaidRenewalTx(ctx, tx, person, order)
	if err != nil {
		return 0, time.Time{}, err
	}
	return n, end, tx.Commit(ctx)
}
func alertsPrepaidRenewalTx(ctx context.Context, tx pgx.Tx, person, order string) (int64, time.Time, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT r.quote_snapshot FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id JOIN service_entitlements e ON e.purchase_id=r.id WHERE r.person_id=$1 AND o.id=$2 AND o.status='paid' AND e.revoked_at IS NULL AND e.product_code='alerts.repository-slots'`, person, order).Scan(&raw)
	if err != nil {
		return 0, time.Time{}, ErrServiceQuote
	}
	var q ServiceQuote
	if json.Unmarshal(raw, &q) != nil || q.Provider != "opennode" || q.BaseOrderID != "" || q.PeriodEnd == nil {
		return 0, time.Time{}, ErrServiceQuote
	}
	var conflict bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id WHERE r.person_id=$1 AND r.quote_snapshot->>'product_code'='alerts.repository-slots' AND o.id<>$2 AND (o.status='pending' AND (o.checkout_expires_at>now() OR EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id)) OR o.status='paid' AND (r.quote_snapshot->>'period_end')::timestamptz>$3))`, person, order, *q.PeriodEnd).Scan(&conflict)
	if err != nil {
		return 0, time.Time{}, err
	}
	if conflict {
		return 0, time.Time{}, ErrAlertsBillingChanged
	}
	var quantity int64
	err = tx.QueryRow(ctx, `SELECT coalesce(sum(e.quantity),0) FROM service_entitlements e JOIN service_purchase_requests r ON r.id=e.purchase_id JOIN shop_orders o ON o.id=r.order_id WHERE e.person_id=$1 AND e.product_code='alerts.repository-slots' AND e.revoked_at IS NULL AND o.status='paid' AND e.ends_at=$3 AND (o.id=$2 OR r.quote_snapshot->>'base_order_id'=$2::text)`, person, order, *q.PeriodEnd).Scan(&quantity)
	return quantity, *q.PeriodEnd, err
}
func validateAlertsRenewalPurchase(ctx context.Context, tx pgx.Tx, person string, q ServiceQuote) error {
	quantity, end, err := alertsPrepaidRenewalTx(ctx, tx, person, q.AlertsRenewOrderID)
	if err != nil {
		return err
	}
	if q.PeriodStart == nil || q.PeriodEnd == nil || q.Provider != "opennode" || q.Quantity != quantity || q.BillingMonths != 12 || q.PeriodStart.Before(end) || q.PeriodStart.Before(time.Now().Add(-15*time.Minute)) {
		return ErrServiceQuote
	}
	term, err := servicebilling.AlertsTerm("opennode", quantity, 12, *q.PeriodStart)
	if err != nil || !term.End.Equal(*q.PeriodEnd) || term.TotalCents != q.TotalCents {
		return ErrServiceQuote
	}
	var conflict bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_checkouts c LEFT JOIN alerts_stripe_subscription_state s ON s.checkout_id=c.id WHERE c.person_id=$1 AND c.closed_at IS NULL AND (s.snapshot IS NULL OR coalesce(s.snapshot->>'providerStatus','')<>'canceled' OR COALESCE((s.snapshot->>'PeriodEnd')::bigint,9223372036854775807)>$2))`, person, q.PeriodStart.Unix()).Scan(&conflict)
	if err != nil {
		return err
	}
	if conflict {
		return ErrAlertsStripeCheckoutConflict
	}
	return nil
}
