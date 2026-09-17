package getters

import (
	"context"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/config"
)

type ServiceReceiptStatus struct {
	PublicID          string    `json:"order"`
	State             string    `json:"state"`
	Attempts          int64     `json:"attempts"`
	RetryAfterAttempt int64     `json:"retry_after_attempt"`
	CreatedAt         time.Time `json:"created_at"`
	NextAttemptAt     time.Time `json:"next_attempt_at"`
	LeaseUntil        time.Time `json:"lease_until"`
}

// ListServiceReceipts exposes operational state without recipients or account IDs.
// With no order filter, only pending/review jobs are returned, oldest first.
func ListServiceReceipts(ctx context.Context, app *config.AppContext, publicID string) ([]ServiceReceiptStatus, error) {
	rows, err := app.DB.Query(ctx, `SELECT o.public_id,j.state,j.attempts,j.retry_after_attempt,j.created_at,j.next_attempt_at,j.lease_until
 FROM service_receipt_jobs j JOIN shop_orders o ON o.id=j.order_id
 WHERE ($1='' AND j.state IN ('pending','review')) OR o.public_id=$1
 ORDER BY j.created_at,j.order_id LIMIT 100`, publicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ServiceReceiptStatus{}
	for rows.Next() {
		var s ServiceReceiptStatus
		if err := rows.Scan(&s.PublicID, &s.State, &s.Attempts, &s.RetryAfterAttempt, &s.CreatedAt, &s.NextAttemptAt, &s.LeaseUntil); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// RecoverServiceReceipt gives a reviewed job another bounded retry budget, with
// an audit record in the same statement. It never sends mail or changes identity.
func RecoverServiceReceipt(ctx context.Context, app *config.AppContext, publicID string, expectedAttempt int64, actor, reason string) (bool, error) {
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if publicID == "" || expectedAttempt < 1 || actor == "" || len(actor) > 200 || reason == "" || len(reason) > 1000 {
		return false, errors.New("order, positive expected attempt, actor and reason required")
	}
	result, err := app.DB.Exec(ctx, `WITH recovered AS (
 UPDATE service_receipt_jobs j SET state='pending',retry_after_attempt=attempts,next_attempt_at=now(),lease_until=now()
 FROM shop_orders o WHERE o.id=j.order_id AND o.public_id=$1 AND o.status='paid'
 AND j.state='review' AND j.attempts=$2 AND j.lease_until<=now()
 RETURNING j.order_id,j.attempts
 ) INSERT INTO service_receipt_recoveries(order_id,after_attempt,actor,reason)
 SELECT order_id,attempts,$3,$4 FROM recovered`, publicID, expectedAttempt, actor, reason)
	return err == nil && result.RowsAffected() == 1, err
}
