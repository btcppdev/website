package getters

import (
	"context"
	"encoding/json"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
)

// ReconcileStoppedServicePaymentCheckouts expires only known one-off Stripe
// sessions. Unknown references and payment-bearing sessions need recovery/review;
// no absence of provider evidence is treated as proof that no payment occurred.
func ReconcileStoppedServicePaymentCheckouts(ctx context.Context, app *config.AppContext, client servicestripe.CheckoutClient) (int, error) {
	rows, err := app.DB.Query(ctx, `WITH due AS (
 SELECT a.id FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id
 JOIN service_sales_closures c ON c.service=split_part(r.quote_snapshot->>'product_code','.',1)
 WHERE a.provider='stripe' AND a.provider_reference LIKE 'cs_%' AND a.settled_at IS NULL AND a.next_reconcile_at<=now()
 AND NOT EXISTS(SELECT 1 FROM service_payment_checkout_expirations e WHERE e.attempt_id=a.id)
 ORDER BY a.next_reconcile_at,a.id LIMIT 20 FOR UPDATE OF a SKIP LOCKED
 ) UPDATE service_payment_attempts a SET next_reconcile_at=now()+interval '15 minutes' FROM due WHERE a.id=due.id RETURNING a.id::text`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var firstErr error
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := expireStoppedServicePaymentCheckout(ctx, app, id, client); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(ids), firstErr
}

func expireStoppedServicePaymentCheckout(ctx context.Context, app *config.AppContext, id string, client servicestripe.CheckoutClient) error {
	var in servicestripe.PaymentCheckout
	err := app.DB.QueryRow(ctx, `SELECT a.id::text,o.id::text,a.provider_reference,o.total_cents FROM service_payment_attempts a
 JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id
 JOIN service_sales_closures c ON c.service=split_part(r.quote_snapshot->>'product_code','.',1)
 WHERE a.id=$1 AND a.provider='stripe' AND a.settled_at IS NULL AND o.currency='USD' AND o.status='pending' AND r.quote_snapshot->>'provider'='stripe' AND o.total_cents=(r.quote_snapshot->>'total_cents')::bigint`, id).Scan(&in.AttemptID, &in.OrderID, &in.Reference, &in.TotalCents)
	if err != nil {
		return err
	}
	evidence, err := client.ExpirePaymentCheckout(ctx, in)
	if err != nil {
		return err
	}
	if evidence == nil || evidence.PaymentCheckout != in || time.Since(evidence.VerifiedAt) < 0 || time.Since(evidence.VerifiedAt) > time.Minute {
		return servicestripe.ErrCheckout
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var valid bool
	err = tx.QueryRow(ctx, `SELECT a.provider='stripe' AND a.provider_reference=$2 AND a.settled_at IS NULL AND o.id=$3 AND o.total_cents=$4 AND o.currency='USD' AND o.status='pending'
 FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id WHERE a.id=$1 FOR UPDATE OF o,a`, id, in.Reference, in.OrderID, in.TotalCents).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrServicePayment
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO service_payment_checkout_expirations(attempt_id,provider_reference,evidence) VALUES($1,$2,$3) ON CONFLICT(attempt_id) DO NOTHING`, id, in.Reference, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
