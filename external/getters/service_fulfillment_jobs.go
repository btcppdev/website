package getters

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"time"
)

type ServiceFulfillmentJob struct {
	AttemptID string
	Attempt   int64
	Payment   VerifiedServicePayment
	Payload   json.RawMessage
}

func ClaimServiceFulfillment(ctx context.Context, app *config.AppContext, attemptID string, now time.Time) (*ServiceFulfillmentJob, error) {
	var job ServiceFulfillmentJob
	var payment []byte
	err := app.DB.QueryRow(ctx, `WITH due AS (
 SELECT attempt_id FROM service_fulfillment_jobs
 WHERE state='pending' AND next_attempt_at<=$2 AND lease_until<=$2
 AND ($1='' OR attempt_id::text=$1)
 ORDER BY next_attempt_at,attempt_id FOR UPDATE SKIP LOCKED LIMIT 1
 ) UPDATE service_fulfillment_jobs j SET attempts=attempts+1,lease_until=$3
 FROM due WHERE j.attempt_id=due.attempt_id
 RETURNING j.attempt_id::text,j.attempts,j.payment,j.payload`, attemptID, now, now.Add(time.Minute)).Scan(&job.AttemptID, &job.Attempt, &payment, &job.Payload)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(payment, &job.Payment); err != nil {
		return nil, err
	}
	return &job, nil
}

// Freeze the first rendered payload before network I/O; retries never rerender it.
func FreezeServiceFulfillment(ctx context.Context, app *config.AppContext, job *ServiceFulfillmentJob, payload []byte) error {
	if job == nil || !json.Valid(payload) {
		return errors.New("valid fulfillment job and payload required")
	}
	return app.DB.QueryRow(ctx, `UPDATE service_fulfillment_jobs SET payload=coalesce(payload,$3::jsonb)
 WHERE attempt_id=$1 AND attempts=$2 AND state='pending' RETURNING payload`, job.AttemptID, job.Attempt, payload).Scan(&job.Payload)
}

func FinishServiceFulfillment(ctx context.Context, app *config.AppContext, job *ServiceFulfillmentJob, state string, now time.Time) (bool, error) {
	if job == nil || job.Attempt < 1 {
		return false, errors.New("fulfillment attempt required")
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
		return false, errors.New("invalid fulfillment outcome")
	}
	result, err := app.DB.Exec(ctx, `UPDATE service_fulfillment_jobs SET state=$3,lease_until=$4,next_attempt_at=$5
 WHERE attempt_id=$1 AND attempts=$2 AND state='pending'`, job.AttemptID, job.Attempt, state, now, now.Add(delay))
	return err == nil && result.RowsAffected() == 1, err
}
