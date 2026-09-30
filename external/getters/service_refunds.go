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
	// AmountSatoshis is verified original-asset evidence for Bitcoin refunds.
	// Nil means unknown, never zero and never a USD conversion at today's rate.
	AmountSatoshis *int64
}

// RecordServiceRefund serializes against settlement and other refunds for the
// order. The original quote, payment event, and settled amount remain immutable.
// Product reversal delivery is a separate step; no provider route calls this yet.
func RecordServiceRefund(ctx context.Context, app *config.AppContext, refund VerifiedServiceRefund) error {
	if refund.AttemptID == "" || (refund.Provider != "stripe" && refund.Provider != "opennode") || len(refund.PaymentReference) < 1 || len(refund.PaymentReference) > 255 || len(refund.RefundReference) < 1 || len(refund.RefundReference) > 255 || refund.AmountCents < 0 || (refund.AmountCents == 0 && refund.AmountSatoshis == nil) || refund.Currency != "USD" || refund.RefundedAt.IsZero() || len(refund.Reason) > 1000 {
		return ErrServiceRefund
	}
	if refund.AmountSatoshis != nil && (refund.Provider != "opennode" || *refund.AmountSatoshis <= 0 || *refund.AmountSatoshis > 2_100_000_000_000_000) {
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
	err = tx.QueryRow(ctx, `SELECT attempt_id=$3 AND payment_reference=$4 AND amount_cents=$5 AND currency=$6 AND refunded_at=$7 AND reason=$8 AND amount_satoshis IS NOT DISTINCT FROM $9::bigint FROM service_payment_refunds WHERE provider=$1 AND refund_reference=$2`, refund.Provider, refund.RefundReference, refund.AttemptID, refund.PaymentReference, refund.AmountCents, refund.Currency, refund.RefundedAt, refund.Reason, refund.AmountSatoshis).Scan(&same)
	if err == nil {
		if !same {
			return ErrServiceRefund
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	// A previous Bitcoin refund may have zero USD cents. Status follows the
	// presence of ledger records, not whether their USD total is positive.
	var refunded, refundCount int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(sum(amount_cents),0),count(*) FROM service_payment_refunds WHERE attempt_id=$1`, refund.AttemptID).Scan(&refunded, &refundCount); err != nil {
		return err
	}
	if refund.RefundedAt.Before(settled) || refunded > total || refund.AmountCents > total-refunded ||
		(refundCount == 0 && status != "paid") || (refundCount > 0 && status != "partially_refunded") {
		return ErrServiceRefund
	}
	if refund.AmountSatoshis != nil {
		var original, largest, unknownPayments, previous, unknownRefunds int64
		err = tx.QueryRow(ctx, `SELECT coalesce(min((opennode_bitcoin->>'price_satoshis')::bigint),0),
 coalesce(max((opennode_bitcoin->>'price_satoshis')::bigint),0),count(*) FILTER(WHERE opennode_bitcoin IS NULL)
 FROM service_payment_events WHERE attempt_id=$1`, refund.AttemptID).Scan(&original, &largest, &unknownPayments)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT coalesce(sum(amount_satoshis),0),count(*) FILTER(WHERE amount_satoshis IS NULL)
 FROM service_payment_refunds WHERE attempt_id=$1`, refund.AttemptID).Scan(&previous, &unknownRefunds)
		if err != nil {
			return err
		}
		if original <= 0 || original != largest || unknownPayments != 0 || unknownRefunds != 0 || previous > original || *refund.AmountSatoshis > original-previous {
			return ErrServiceRefund
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_payment_refunds(provider,refund_reference,attempt_id,payment_reference,amount_cents,currency,refunded_at,reason,amount_satoshis) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, refund.Provider, refund.RefundReference, refund.AttemptID, refund.PaymentReference, refund.AmountCents, refund.Currency, refund.RefundedAt, refund.Reason, refund.AmountSatoshis)
	if err != nil {
		return err
	}
	status = "partially_refunded"
	if _, err = tx.Exec(ctx, `INSERT INTO service_refund_jobs(provider,refund_reference,attempt_id,revision)
 SELECT $1,$2,$3,count(*) FROM service_payment_refunds WHERE attempt_id=$3`, refund.Provider, refund.RefundReference, refund.AttemptID); err != nil {
		return err
	}
	if refunded+refund.AmountCents == total {
		status = "refunded"
	}
	if _, err = tx.Exec(ctx, `UPDATE shop_orders SET status=$2 WHERE id=$1`, order, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
