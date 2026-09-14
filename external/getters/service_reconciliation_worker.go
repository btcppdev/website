package getters

import (
	"context"
	"time"

	"btcpp-web/internal/config"
)

// ReconcileDueOpenNodeServices processes at most twenty known charges. Selection
// advances the durable schedule atomically so multiple servers do not all poll
// the same charge. A crash is retried when that schedule becomes due again.
// Unknown charge IDs require provider/manual recovery; this never creates charges.
func ReconcileDueOpenNodeServices(ctx context.Context, app *config.AppContext) (int, error) {
	return reconcileDueOpenNodeServices(ctx, app, ReconcileOpenNodeServiceCharge)
}

func reconcileDueOpenNodeServices(ctx context.Context, app *config.AppContext, reconcile func(context.Context, *config.AppContext, string) (bool, error)) (int, error) {
	rows, err := app.DB.Query(ctx, `WITH due AS (
		SELECT a.id FROM service_payment_attempts a
		JOIN service_purchase_requests r ON r.id=a.purchase_id
		JOIN shop_orders o ON o.id=r.order_id
		WHERE a.provider='opennode' AND a.provider_reference IS NOT NULL AND a.next_reconcile_at<=now()
		AND (a.settled_at IS NULL OR (o.status='paid' AND (NOT EXISTS (
			SELECT 1 FROM service_entitlements e WHERE e.purchase_id=r.id)
			OR (EXISTS(SELECT 1 FROM tax_quotes t WHERE t.order_id=o.id AND t.sales_tax_provider='stripe')
			AND NOT EXISTS(SELECT 1 FROM tax_transactions t WHERE t.order_id=o.id AND t.provider='stripe' AND t.status='recorded')))))
		ORDER BY a.next_reconcile_at,a.id LIMIT 20 FOR UPDATE OF a SKIP LOCKED
	) UPDATE service_payment_attempts a SET next_reconcile_at=now()+CASE
		WHEN a.created_at<now()-interval '7 days' THEN interval '1 day' ELSE interval '15 minutes' END
	FROM due WHERE a.id=due.id RETURNING a.provider_reference`)
	if err != nil {
		return 0, err
	}
	var references []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return 0, err
		}
		references = append(references, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	// Release database rows before provider I/O. One failure must not prevent
	// checking the rest of the batch; return the first error for monitoring.
	var firstErr error
	completed := 0
	for _, ref := range references {
		if err := ctx.Err(); err != nil {
			return completed, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err := reconcile(attemptCtx, app, ref)
		cancel()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		completed++
	}
	return completed, firstErr
}
