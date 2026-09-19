package getters

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"time"
)

type ServiceRefundJob struct {
	Refund            VerifiedServiceRefund
	Revision, Attempt int64
	Payload           json.RawMessage
}

// Claims preserve refund acceptance order. Insider reversals cannot overtake
// original settlement acknowledgment; an earlier review blocks later refunds.
// No running worker calls this until product adjustment semantics are ready.
func ClaimInsiderRefundDelivery(ctx context.Context, app *config.AppContext, attemptID string, now time.Time) (*ServiceRefundJob, error) {
	var job ServiceRefundJob
	err := app.DB.QueryRow(ctx, `WITH due AS (
 SELECT j.provider,j.refund_reference FROM service_refund_jobs j
 JOIN service_fulfillment_jobs original ON original.attempt_id=j.attempt_id AND original.state='accepted'
 JOIN service_payment_attempts a ON a.id=j.attempt_id
 JOIN service_purchase_requests r ON r.id=a.purchase_id
 WHERE j.state='pending' AND j.next_attempt_at<=$2 AND j.lease_until<=$2
 AND r.quote_snapshot->>'product_code' LIKE 'insider.%'
 AND ($1='' OR j.attempt_id::text=$1)
 AND NOT EXISTS(SELECT 1 FROM service_refund_jobs prior WHERE prior.attempt_id=j.attempt_id AND prior.revision<j.revision AND prior.state<>'accepted')
 ORDER BY j.next_attempt_at,j.attempt_id,j.revision FOR UPDATE OF j SKIP LOCKED LIMIT 1
 ), claimed AS (
 UPDATE service_refund_jobs j SET attempts=attempts+1,lease_until=$3 FROM due
 WHERE j.provider=due.provider AND j.refund_reference=due.refund_reference RETURNING j.*
 ) SELECT j.attempt_id::text,j.provider,j.refund_reference,r.payment_reference,r.amount_cents,r.currency,r.refunded_at,r.reason,j.revision,j.attempts,j.payload
 FROM claimed j JOIN service_payment_refunds r USING(provider,refund_reference)`, attemptID, now, now.Add(time.Minute)).Scan(&job.Refund.AttemptID, &job.Refund.Provider, &job.Refund.RefundReference, &job.Refund.PaymentReference, &job.Refund.AmountCents, &job.Refund.Currency, &job.Refund.RefundedAt, &job.Refund.Reason, &job.Revision, &job.Attempt, &job.Payload)
	return &job, err
}

func FreezeServiceRefundDelivery(ctx context.Context, app *config.AppContext, job *ServiceRefundJob, payload []byte) error {
	if job == nil || !json.Valid(payload) {
		return errors.New("valid refund job and payload required")
	}
	return app.DB.QueryRow(ctx, `UPDATE service_refund_jobs SET payload=coalesce(payload,$4::jsonb)
 WHERE provider=$1 AND refund_reference=$2 AND attempts=$3 AND state='pending' RETURNING payload`, job.Refund.Provider, job.Refund.RefundReference, job.Attempt, payload).Scan(&job.Payload)
}

func FinishServiceRefundDelivery(ctx context.Context, app *config.AppContext, job *ServiceRefundJob, state string, now time.Time) (bool, error) {
	if job == nil || job.Attempt < 1 {
		return false, errors.New("refund delivery attempt required")
	}
	delay := time.Minute
	switch state {
	case "accepted", "review":
	case "retry":
		state = "pending"
		for i := int64(1); i < job.Attempt && delay < time.Hour; i++ {
			delay *= 2
		}
		if delay > time.Hour {
			delay = time.Hour
		}
	default:
		return false, errors.New("invalid refund delivery outcome")
	}
	result, err := app.DB.Exec(ctx, `UPDATE service_refund_jobs SET state=$4,lease_until=$5,next_attempt_at=$6
 WHERE provider=$1 AND refund_reference=$2 AND attempts=$3 AND state='pending'`, job.Refund.Provider, job.Refund.RefundReference, job.Attempt, state, now, now.Add(delay))
	return err == nil && result.RowsAffected() == 1, err
}
