package getters

import (
	"btcpp-web/internal/config"
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrServiceRefundAdmin = errors.New("global administrator required for refund access decision")

// RecordServiceRefundAccessDecision authorizes an exceptional refund's access
// effect. Actor must come from authenticated server identity, not form data.
// It neither initiates a refund nor cancels a recurring subscription.
func RecordServiceRefundAccessDecision(ctx context.Context, app *config.AppContext, actor, provider, reference, action, reason string) error {
	reason = strings.TrimSpace(reason)
	if (action != "retain" && action != "revoke") || reason == "" || len(reason) > 1000 {
		return ErrServiceRefund
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role int
	err = tx.QueryRow(ctx, `SELECT 1 FROM people_roles WHERE person_id=$1 AND scope='global' AND position='admin' FOR SHARE`, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServiceRefundAdmin
	}
	if err != nil {
		return err
	}
	var order string
	err = tx.QueryRow(ctx, `SELECT o.id::text FROM service_payment_refunds f
 JOIN service_payment_attempts a ON a.id=f.attempt_id JOIN service_purchase_requests r ON r.id=a.purchase_id
 JOIN shop_orders o ON o.id=r.order_id WHERE f.provider=$1 AND f.refund_reference=$2 FOR UPDATE OF o`, provider, reference).Scan(&order)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServiceRefund
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_refund_access_decisions(provider,refund_reference,action,actor_id,reason) VALUES($1,$2,$3,$4,$5) ON CONFLICT(provider,refund_reference) DO NOTHING`, provider, reference, action, actor, reason)
	if err != nil {
		return err
	}
	var same bool
	if err = tx.QueryRow(ctx, `SELECT action=$3 AND actor_id=$4 AND reason=$5 FROM service_refund_access_decisions WHERE provider=$1 AND refund_reference=$2`, provider, reference, action, actor, reason).Scan(&same); err != nil {
		return err
	}
	if !same {
		return ErrServiceRefund
	}
	return tx.Commit(ctx)
}
