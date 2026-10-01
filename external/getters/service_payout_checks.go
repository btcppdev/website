package getters

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"btcpp-web/internal/bitcoinpayout"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BitcoinPayoutCheck struct {
	State       string     `json:"state"`
	CheckedAt   *time.Time `json:"checked_at"`
	NextCheckAt time.Time  `json:"next_check_at"`
}

func InspectBitcoinPayoutCheck(ctx context.Context, app *config.AppContext, review string) (*BitcoinPayoutCheck, error) {
	out := &BitcoinPayoutCheck{}
	err := app.DB.QueryRow(ctx, `SELECT state,checked_at,next_check_at FROM service_bitcoin_payout_checks WHERE review_id=$1`, review).Scan(&out.State, &out.CheckedAt, &out.NextCheckAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &BitcoinPayoutCheck{State: "pending"}, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

type bitcoinPayoutCheckJob struct {
	Review, Token string
	Original      bitcoinpayout.Evidence
}

// RecheckDueBitcoinPayouts observes already recorded outputs. Failure to verify
// means investigation is needed, not proof of a reorg and never permission to pay
// again. No financial record or product access is changed by this worker.
func RecheckDueBitcoinPayouts(ctx context.Context, app *config.AppContext, node *bitcoinpayout.Client) (int, error) {
	if node == nil {
		return 0, bitcoinpayout.ErrUnverified
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO service_bitcoin_payout_checks(review_id) SELECT review_id FROM service_bitcoin_closure_payouts ON CONFLICT DO NOTHING`); err != nil {
		return 0, err
	}
	for n := 0; n < 20; n++ {
		job, err := claimBitcoinPayoutCheck(ctx, app, time.Now().UTC())
		if err != nil {
			return n, err
		}
		if job == nil {
			return n, nil
		}
		callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		evidence, verifyErr := node.Verify(callCtx, job.Original.Request)
		cancel()
		if err := finishBitcoinPayoutCheck(ctx, app, job, evidence, verifyErr, time.Now().UTC()); err != nil {
			return n, err
		}
	}
	return 20, nil
}

func claimBitcoinPayoutCheck(ctx context.Context, app *config.AppContext, now time.Time) (*bitcoinPayoutCheckJob, error) {
	job := &bitcoinPayoutCheckJob{Token: uuid.NewString()}
	var raw []byte
	err := app.DB.QueryRow(ctx, `WITH due AS (
 SELECT review_id FROM service_bitcoin_payout_checks WHERE next_check_at<=$1 AND lease_until<=$1 ORDER BY next_check_at,review_id LIMIT 1 FOR UPDATE SKIP LOCKED
 ), claimed AS (
 UPDATE service_bitcoin_payout_checks c SET lease_token=$2,lease_until=$1::timestamptz+interval '2 minutes' FROM due WHERE c.review_id=due.review_id RETURNING c.review_id
 ) SELECT p.review_id::text,p.evidence FROM claimed c JOIN service_bitcoin_closure_payouts p USING(review_id)`, now, job.Token).Scan(&job.Review, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &job.Original); err != nil {
		return nil, err
	}
	return job, nil
}

func finishBitcoinPayoutCheck(ctx context.Context, app *config.AppContext, job *bitcoinPayoutCheckJob, evidence *bitcoinpayout.Evidence, verifyErr error, now time.Time) error {
	state := "unverified"
	next := now.Add(15 * time.Minute)
	var raw []byte
	if verifyErr == nil && evidence != nil && evidence.Request == job.Original.Request && evidence.Chain == job.Original.Chain && evidence.ScriptPubKey == job.Original.ScriptPubKey && evidence.Confirmations >= bitcoinpayout.RequiredConfirmations && !evidence.VerifiedAt.After(now) && now.Sub(evidence.VerifiedAt) < time.Minute {
		state = "confirmed"
		next = now.Add(6 * time.Hour)
		var err error
		raw, err = json.Marshal(evidence)
		if err != nil {
			return err
		}
	}
	result, err := app.DB.Exec(ctx, `UPDATE service_bitcoin_payout_checks SET state=$3,checked_at=$4,evidence=$5::jsonb,next_check_at=$6,lease_token=NULL,lease_until=$4 WHERE review_id=$1 AND lease_token=$2`, job.Review, job.Token, state, now, raw, next)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return bitcoinpayout.ErrUnverified
	}
	return nil
}
