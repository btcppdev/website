package getters

import (
	"btcpp-web/internal/config"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrServiceRefund = errors.New("refund does not match verified service payment")

// VerifiedServiceRefund is trusted adapter output from an authenticated provider
// lookup. Recording it never initiates a refund at the provider.
type VerifiedServiceRefund struct {
	AttemptID, Provider, PaymentReference, RefundReference string
	AmountCents                                            int64
	Currency                                               string
	RefundedAt                                             time.Time
	Reason                                                 string
}

// RecordServiceRefund serializes against settlement and other refunds for the
// order. The original quote, payment event, and settled amount remain immutable.
// Product reversal delivery is a separate step; no provider route calls this yet.
func RecordServiceRefund(ctx context.Context, app *config.AppContext, refund VerifiedServiceRefund) error {
	if refund.AttemptID == "" || (refund.Provider != "stripe" && refund.Provider != "opennode") || len(refund.PaymentReference) < 1 || len(refund.PaymentReference) > 255 || len(refund.RefundReference) < 1 || len(refund.RefundReference) > 255 || refund.AmountCents <= 0 || refund.Currency != "USD" || refund.RefundedAt.IsZero() || len(refund.Reason) > 1000 {
		return ErrServiceRefund
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var order, status string
	var total int64
	var settled time.Time
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.status,o.total_cents,a.settled_at
 FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id
 WHERE a.id=$1 AND a.provider=$2 AND a.provider_reference=$3 AND a.settled_at IS NOT NULL
 AND o.payment_provider=a.provider AND o.payment_provider_id=a.provider_reference AND o.currency=$4
 AND o.total_cents=(r.quote_snapshot->>'total_cents')::bigint
 FOR UPDATE OF o,a`, refund.AttemptID, refund.Provider, refund.PaymentReference, refund.Currency).Scan(&order, &status, &total, &settled)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServiceRefund
	}
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT attempt_id=$3 AND payment_reference=$4 AND amount_cents=$5 AND currency=$6 AND refunded_at=$7 AND reason=$8 FROM service_payment_refunds WHERE provider=$1 AND refund_reference=$2`, refund.Provider, refund.RefundReference, refund.AttemptID, refund.PaymentReference, refund.AmountCents, refund.Currency, refund.RefundedAt, refund.Reason).Scan(&same)
	if err == nil {
		if !same {
			return ErrServiceRefund
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var refunded int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(sum(amount_cents),0) FROM service_payment_refunds WHERE attempt_id=$1`, refund.AttemptID).Scan(&refunded); err != nil {
		return err
	}
	if refund.RefundedAt.Before(settled) || refunded > total || refund.AmountCents > total-refunded ||
		(refunded == 0 && status != "paid") || (refunded > 0 && refunded < total && status != "partially_refunded") {
		return ErrServiceRefund
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_payment_refunds(provider,refund_reference,attempt_id,payment_reference,amount_cents,currency,refunded_at,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, refund.Provider, refund.RefundReference, refund.AttemptID, refund.PaymentReference, refund.AmountCents, refund.Currency, refund.RefundedAt, refund.Reason)
	if err != nil {
		return err
	}
	status = "partially_refunded"
	if refunded+refund.AmountCents == total {
		status = "refunded"
	}
	if _, err = tx.Exec(ctx, `UPDATE shop_orders SET status=$2 WHERE id=$1`, order, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
