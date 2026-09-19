package getters

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"time"
)

type ServiceLifecycleJob struct {
	CheckoutID        string
	Revision, Attempt int64
	Payload           json.RawMessage
}

// A checkout's next revision waits for all earlier events to be acknowledged.
// Review also blocks subsequent revisions until an operator resolves the event.
func ClaimServiceLifecycle(ctx context.Context, app *config.AppContext, checkoutID string, now time.Time) (*ServiceLifecycleJob, error) {
	var job ServiceLifecycleJob
	err := app.DB.QueryRow(ctx, `WITH due AS (
 SELECT j.checkout_id,j.revision FROM service_lifecycle_jobs j
 WHERE j.state='pending' AND j.next_attempt_at<=$2 AND j.lease_until<=$2
 AND ($1='' OR j.checkout_id::text=$1)
 AND NOT EXISTS (SELECT 1 FROM service_lifecycle_jobs prior
 WHERE prior.checkout_id=j.checkout_id AND prior.revision<j.revision AND prior.state<>'accepted')
 ORDER BY j.next_attempt_at,j.checkout_id,j.revision FOR UPDATE OF j SKIP LOCKED LIMIT 1
 ) UPDATE service_lifecycle_jobs j SET attempts=attempts+1,lease_until=$3
 FROM due WHERE j.checkout_id=due.checkout_id AND j.revision=due.revision
 RETURNING j.checkout_id::text,j.revision,j.attempts,j.event`, checkoutID, now, now.Add(time.Minute)).Scan(&job.CheckoutID, &job.Revision, &job.Attempt, &job.Payload)
	return &job, err
}

func ServiceLifecycleAccountExists(ctx context.Context, app *config.AppContext, job *ServiceLifecycleJob) (bool, error) {
	var exists bool
	err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_stripe_checkouts c
 JOIN service_lifecycle_jobs j ON j.checkout_id=c.id
 WHERE j.checkout_id=$1 AND j.revision=$2 AND c.person_id::text=j.event->>'person_id'
 AND c.product_code=j.event->>'product_code' AND c.product_family='insider')`, job.CheckoutID, job.Revision).Scan(&exists)
	return exists, err
}

func FinishServiceLifecycle(ctx context.Context, app *config.AppContext, job *ServiceLifecycleJob, state string, now time.Time) (bool, error) {
	if job == nil || job.Attempt < 1 {
		return false, errors.New("lifecycle attempt required")
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
		return false, errors.New("invalid lifecycle outcome")
	}
	result, err := app.DB.Exec(ctx, `UPDATE service_lifecycle_jobs SET state=$4,lease_until=$5,next_attempt_at=$6
 WHERE checkout_id=$1 AND revision=$2 AND attempts=$3 AND state='pending'`, job.CheckoutID, job.Revision, job.Attempt, state, now, now.Add(delay))
	return err == nil && result.RowsAffected() == 1, err
}
