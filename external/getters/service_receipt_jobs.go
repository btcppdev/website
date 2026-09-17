package getters

import (
	"context"
	"errors"
	"time"

	"btcpp-web/internal/config"
)

type ServiceReceiptJob struct {
	OrderID, PublicID, PersonID, Recipient string
	Attempt                                int64
}

// ClaimServiceReceipt commits a bounded lease before any rendering or mail I/O.
// orderID can narrow an operator/test recovery attempt; empty chooses any due job.
func ClaimServiceReceipt(ctx context.Context, app *config.AppContext, orderID string, now time.Time) (*ServiceReceiptJob, error) {
	var job ServiceReceiptJob
	if _, err := app.DB.Exec(ctx, `UPDATE service_receipt_jobs SET state='review' WHERE state='pending' AND attempts>=12 AND lease_until<=$2 AND ($1='' OR order_id::text=$1)`, orderID, now); err != nil {
		return nil, err
	}
	err := app.DB.QueryRow(ctx, `WITH due AS (
 SELECT order_id FROM service_receipt_jobs WHERE state='pending'
 AND attempts<12 AND next_attempt_at<=$2 AND lease_until<=$2 AND ($1='' OR order_id::text=$1)
 ORDER BY next_attempt_at,order_id FOR UPDATE SKIP LOCKED LIMIT 1
 ), claimed AS (
 UPDATE service_receipt_jobs j SET attempts=attempts+1,lease_until=$3 FROM due
 WHERE j.order_id=due.order_id RETURNING j.order_id,j.recipient,j.attempts
 ) SELECT c.order_id::text,o.public_id,r.person_id::text,c.recipient,c.attempts
 FROM claimed c JOIN shop_orders o ON o.id=c.order_id JOIN service_purchase_requests r ON r.order_id=o.id`, orderID, now, now.Add(2*time.Minute)).Scan(&job.OrderID, &job.PublicID, &job.PersonID, &job.Recipient, &job.Attempt)
	return &job, err
}

// FinishServiceReceipt fences late responses using the committed attempt number.
// A provider timeout must use retry with the original receipt job key.
func FinishServiceReceipt(ctx context.Context, app *config.AppContext, job *ServiceReceiptJob, outcome string, now time.Time) (bool, error) {
	if job == nil || job.Attempt < 1 {
		return false, errors.New("receipt attempt required")
	}
	state := outcome
	var accepted *time.Time
	next := now
	switch outcome {
	case "accepted":
		accepted = &now
	case "cancelled", "review":
	case "retry":
		state = "pending"
		if job.Attempt >= 12 {
			state = "review"
		}
		delay := 5 * time.Minute
		for i := int64(1); i < job.Attempt && delay < 6*time.Hour; i++ {
			delay *= 2
		}
		if delay > 6*time.Hour {
			delay = 6 * time.Hour
		}
		next = now.Add(delay)
	default:
		return false, errors.New("invalid receipt outcome")
	}
	result, err := app.DB.Exec(ctx, `UPDATE service_receipt_jobs SET state=$3,next_attempt_at=$4,lease_until=$5,accepted_at=$6
 WHERE order_id=$1 AND attempts=$2 AND state='pending'`, job.OrderID, job.Attempt, state, next, now, accepted)
	return err == nil && result.RowsAffected() == 1, err
}
