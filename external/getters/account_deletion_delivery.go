package getters

import (
	"btcpp-web/internal/config"
	"context"
	"errors"
	"time"
)

type AccountDeletionDelivery struct {
	PersonID string
	Service  string
	Attempt  int64
}

func ClaimAccountDeletionDelivery(ctx context.Context, app *config.AppContext, personID, service string, now time.Time) (*AccountDeletionDelivery, error) {
	if service != "alerts" && service != "insider" {
		return nil, errors.New("unknown account deletion service")
	}
	job := AccountDeletionDelivery{Service: service}
	err := app.DB.QueryRow(ctx, `WITH due AS (
	 SELECT person_id FROM account_deletion_deliveries WHERE service=$4 AND state='pending' AND next_attempt_at<=$2 AND lease_until<=$2
	 AND ($1='' OR person_id::text=$1) ORDER BY next_attempt_at,person_id FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE account_deletion_deliveries j SET attempts=attempts+1,lease_until=$3 FROM due
	 WHERE j.person_id=due.person_id AND j.service=$4 RETURNING j.person_id::text,j.attempts`, personID, now, now.Add(time.Minute), service).Scan(&job.PersonID, &job.Attempt)
	return &job, err
}

func FinishAccountDeletionDelivery(ctx context.Context, app *config.AppContext, job *AccountDeletionDelivery, accepted bool, now time.Time) (bool, error) {
	if job == nil || job.Attempt < 1 || (job.Service != "alerts" && job.Service != "insider") {
		return false, errors.New("account deletion attempt required")
	}
	state := "pending"
	if accepted {
		state = "accepted"
	}
	delay := time.Minute
	for i := int64(1); i < job.Attempt && delay < time.Hour; i++ {
		delay *= 2
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	result, err := app.DB.Exec(ctx, `UPDATE account_deletion_deliveries SET state=$3,next_attempt_at=$4,lease_until=$5
	 WHERE person_id=$1 AND service=$6 AND attempts=$2 AND state='pending'`, job.PersonID, job.Attempt, state, now.Add(delay), now, job.Service)
	return err == nil && result.RowsAffected() == 1, err
}
