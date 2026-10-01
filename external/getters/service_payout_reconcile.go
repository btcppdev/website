package getters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/bitcoinpayout"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReconcileBitcoinPayoutBlock records a new, verified block for the same paid
// output. It never changes the original payout, refund balance or product access.
// Actor must be derived from server authentication. ID identifies this immutable
// review; exact retries return successfully even after another reconciliation.
func ReconcileBitcoinPayoutBlock(ctx context.Context, app *config.AppContext, node *bitcoinpayout.Client, actor, id, review, block, reason string) error {
	for _, value := range []string{actor, id, review} {
		if parsed, err := uuid.Parse(value); err != nil || parsed.String() != value {
			return ErrServiceRefund
		}
	}
	reason = strings.TrimSpace(reason)
	if node == nil || reason == "" || len(reason) > 1000 {
		return ErrServiceRefund
	}
	var authorized bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account)`, actor).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return ErrServiceRefundAdmin
	}
	replay := func(query interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}) (bool, error) {
		var same bool
		err := query.QueryRow(ctx, `SELECT actor_id=$2 AND review_id=$3 AND block_hash=$4 AND reason=$5 FROM service_bitcoin_payout_reconciliations WHERE id=$1`, id, actor, review, block, reason).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !same {
			return false, ErrServiceRefund
		}
		return true, nil
	}
	if done, err := replay(app.DB); done || err != nil {
		return err
	}
	var raw []byte
	var previousID string
	if err := app.DB.QueryRow(ctx, `SELECT coalesce(r.evidence,p.evidence),coalesce(c.reconciliation_id::text,'') FROM service_bitcoin_closure_payouts p LEFT JOIN service_bitcoin_payout_checks c USING(review_id) LEFT JOIN service_bitcoin_payout_reconciliations r ON r.id=c.reconciliation_id WHERE p.review_id=$1`, review).Scan(&raw, &previousID); err != nil {
		return err
	}
	var previous bitcoinpayout.Evidence
	if err := json.Unmarshal(raw, &previous); err != nil {
		return err
	}
	if block == previous.BlockHash || (previous.Chain != "main" && (app.Env == nil || app.Env.Prod)) {
		return ErrServiceRefund
	}
	request := previous.Request
	request.BlockHash = block
	evidence, err := node.Verify(ctx, request)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if evidence == nil || evidence.Request != request || evidence.Chain != previous.Chain || evidence.ScriptPubKey != previous.ScriptPubKey || evidence.Confirmations < bitcoinpayout.RequiredConfirmations || evidence.VerifiedAt.After(now) || now.Sub(evidence.VerifiedAt) > time.Minute {
		return ErrServiceRefund
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role int
	err = tx.QueryRow(ctx, `SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account FOR SHARE OF r,p`, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServiceRefundAdmin
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO service_bitcoin_payout_checks(review_id) VALUES($1) ON CONFLICT DO NOTHING`, review); err != nil {
		return err
	}
	var currentID string
	if err := tx.QueryRow(ctx, `SELECT coalesce(reconciliation_id::text,'') FROM service_bitcoin_payout_checks WHERE review_id=$1 FOR UPDATE`, review).Scan(&currentID); err != nil {
		return err
	}
	if done, err := replay(tx); done || err != nil {
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if currentID != previousID || time.Since(evidence.VerifiedAt) > time.Minute {
		return ErrServiceRefund
	}
	raw, err = json.Marshal(evidence)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO service_bitcoin_payout_reconciliations(id,review_id,actor_id,previous_block,block_hash,evidence,reason) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7)`, id, review, actor, previous.BlockHash, block, raw, reason); err != nil {
		return err
	}
	// Clear the old lease so a worker checking the former block cannot overwrite
	// this accepted observation after network I/O completes.
	if _, err := tx.Exec(ctx, `UPDATE service_bitcoin_payout_checks SET reconciliation_id=$2,state='confirmed',checked_at=$3,evidence=$4::jsonb,next_check_at=$3::timestamptz+interval '6 hours',lease_until=$3,lease_token=NULL WHERE review_id=$1`, review, id, now, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
