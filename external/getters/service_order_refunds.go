package getters

import (
	"btcpp-web/internal/config"
	"context"
)

type ServiceOrderRefundSummary struct {
	IsService      bool
	RefundedCents  uint
	RemainingCents uint
	AttemptID      string
}

func ServiceOrderRefunds(ctx context.Context, app *config.AppContext, order string) (ServiceOrderRefundSummary, error) {
	var s ServiceOrderRefundSummary
	err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_purchase_requests WHERE order_id=o.id),
 coalesce((SELECT sum(f.amount_cents) FROM service_payment_refunds f JOIN service_payment_attempts a ON a.id=f.attempt_id JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.order_id=o.id),0),
 coalesce((SELECT a.id::text FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.order_id=o.id AND a.settled_at IS NOT NULL ORDER BY a.settled_at DESC,a.id LIMIT 1),''),
 o.total_cents FROM shop_orders o WHERE o.id=$1`, order).Scan(&s.IsService, &s.RefundedCents, &s.AttemptID, &s.RemainingCents)
	if s.RefundedCents > s.RemainingCents {
		return s, ErrServiceRefund
	}
	s.RemainingCents -= s.RefundedCents
	return s, err
}
