package getters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"btcpp-web/internal/bitcoinpayout"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type BitcoinClosurePayoutRequest struct {
	ReviewID  string `json:"review_id"`
	TxID      string `json:"txid"`
	Vout      uint32 `json:"vout"`
	BlockHash string `json:"block_hash"`
}

type BitcoinClosurePayout struct {
	TopUpSatoshis int64                   `json:"top_up_satoshis"`
	ReviewID      string                  `json:"review_id"`
	ActorID       string                  `json:"actor_id"`
	Evidence      *bitcoinpayout.Evidence `json:"evidence"`
	Refund        VerifiedServiceRefund   `json:"refund"`
	RecordedAt    time.Time               `json:"recorded_at"`
}

func InspectBitcoinClosurePayout(ctx context.Context, app *config.AppContext, reviewID string) (*BitcoinClosurePayout, error) {
	return inspectBitcoinClosurePayout(ctx, app.DB, reviewID)
}

func inspectBitcoinClosurePayout(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, reviewID string) (*BitcoinClosurePayout, error) {
	out := &BitcoinClosurePayout{ReviewID: reviewID}
	var raw []byte
	err := query.QueryRow(ctx, `SELECT p.actor_id::text,p.evidence,p.recorded_at,p.top_up_satoshis,f.attempt_id::text,f.provider,f.payment_reference,f.refund_reference,f.amount_cents,f.currency,f.refunded_at,f.reason,f.amount_satoshis FROM service_bitcoin_closure_payouts p JOIN service_payment_refunds f ON f.provider=p.provider AND f.refund_reference=p.refund_reference WHERE p.review_id=$1`, reviewID).Scan(&out.ActorID, &raw, &out.RecordedAt, &out.TopUpSatoshis, &out.Refund.AttemptID, &out.Refund.Provider, &out.Refund.PaymentReference, &out.Refund.RefundReference, &out.Refund.AmountCents, &out.Refund.Currency, &out.Refund.RefundedAt, &out.Refund.Reason, &out.Refund.AmountSatoshis)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.Evidence); err != nil {
		return nil, err
	}
	return out, nil
}

func matchingBitcoinPayout(p *BitcoinClosurePayout, actor string, req BitcoinClosurePayoutRequest) bool {
	return p != nil && p.Evidence != nil && p.ActorID == actor && p.ReviewID == req.ReviewID && p.Evidence.TxID == req.TxID && p.Evidence.Vout == req.Vout && p.Evidence.BlockHash == req.BlockHash
}

// RecordBitcoinClosurePayout verifies a payment already made by the admin's
// wallet, then atomically records its output allocation and the shared refund/job.
// Actor must come from authenticated server identity. This function never pays,
// reverses tax, cancels renewal or decides product access. Existing access-decision
// and closure orchestration flows still apply. A replay returns the original
// observation, not an assertion that its block remains confirmed today.
func RecordBitcoinClosurePayout(ctx context.Context, app *config.AppContext, node *bitcoinpayout.Client, actor string, req BitcoinClosurePayoutRequest) (*BitcoinClosurePayout, error) {
	for _, value := range []string{actor, req.ReviewID} {
		if id, err := uuid.Parse(value); err != nil || id.String() != value {
			return nil, ErrServiceRefund
		}
	}
	if node == nil {
		return nil, ErrServiceRefund
	}
	var authorized bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account)`, actor).Scan(&authorized); err != nil {
		return nil, err
	}
	if !authorized {
		return nil, ErrServiceRefundAdmin
	}
	prior, err := inspectBitcoinClosurePayout(ctx, app.DB, req.ReviewID)
	if err == nil {
		if !matchingBitcoinPayout(prior, actor, req) {
			return nil, ErrServiceRefund
		}
		return prior, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	review, err := InspectBitcoinClosureReview(ctx, app, req.ReviewID)
	if err != nil {
		return nil, err
	}
	destination, err := InspectBitcoinClosureDestination(ctx, app, req.ReviewID)
	if err != nil {
		return nil, err
	}
	if review.Basis == nil || review.Basis.RemainingRefundSatoshis == nil || review.Basis.USDAllocation == nil || (destination.Destination.Chain != "main" && (app.Env == nil || app.Env.Prod)) {
		return nil, ErrServiceRefund
	}
	evidence, err := node.Verify(ctx, bitcoinpayout.Request{TxID: req.TxID, Vout: req.Vout, BlockHash: req.BlockHash, Destination: destination.Destination.Address, Satoshis: destination.PayoutSatoshis})
	if err != nil {
		return nil, err
	}
	return recordVerifiedBitcoinClosurePayout(ctx, app, actor, req, evidence)
}

func recordVerifiedBitcoinClosurePayout(ctx context.Context, app *config.AppContext, actor string, req BitcoinClosurePayoutRequest, evidence *bitcoinpayout.Evidence) (*BitcoinClosurePayout, error) {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var role int
	err = tx.QueryRow(ctx, `SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account FOR SHARE OF r,p`, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServiceRefundAdmin
	}
	if err != nil {
		return nil, err
	}
	var raw []byte
	var attempt, reference string
	var closure time.Time
	err = tx.QueryRow(ctx, `SELECT v.attempt_id::text,v.closure_at,v.basis,a.provider_reference FROM service_bitcoin_closure_reviews v JOIN service_payment_attempts a ON a.id=v.attempt_id JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id WHERE v.id=$1 FOR UPDATE OF o,a`, req.ReviewID).Scan(&attempt, &closure, &raw, &reference)
	if err != nil {
		return nil, err
	}
	prior, err := inspectBitcoinClosurePayout(ctx, tx, req.ReviewID)
	if err == nil {
		if !matchingBitcoinPayout(prior, actor, req) {
			return nil, ErrServiceRefund
		}
		return prior, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	destination, err := inspectBitcoinClosureDestination(ctx, tx, req.ReviewID)
	if err != nil {
		return nil, err
	}
	plan, err := planBitcoinClosureRefund(ctx, tx, attempt, closure)
	if err != nil {
		return nil, err
	}
	var reviewed BitcoinClosurePlan
	if err = json.Unmarshal(raw, &reviewed); err != nil {
		return nil, err
	}
	expected, _ := json.Marshal(reviewed)
	current, _ := json.Marshal(plan)
	if !bytes.Equal(expected, current) || len(plan.ReviewReasons) != 0 || plan.RemainingRefundSatoshis == nil || *plan.RemainingRefundSatoshis <= 0 || plan.USDAllocation == nil {
		return nil, ErrServiceRefund
	}
	if evidence == nil || evidence.TxID != req.TxID || evidence.Vout != req.Vout || evidence.BlockHash != req.BlockHash || evidence.Satoshis != destination.PayoutSatoshis || evidence.Satoshis-destination.TopUpSatoshis != *plan.RemainingRefundSatoshis || evidence.Chain != destination.Destination.Chain || evidence.Destination != destination.Destination.Address || evidence.ScriptPubKey != destination.Destination.ScriptPubKey || evidence.Confirmations < bitcoinpayout.RequiredConfirmations || evidence.RequiredConfirmations != bitcoinpayout.RequiredConfirmations || evidence.VerifiedAt.IsZero() {
		return nil, ErrServiceRefund
	}
	age := time.Since(evidence.VerifiedAt)
	if age < 0 || age > time.Minute || (evidence.Chain != "main" && (app.Env == nil || app.Env.Prod)) {
		return nil, ErrServiceRefund
	}
	refund := VerifiedServiceRefund{AttemptID: attempt, Provider: "opennode", PaymentReference: reference, RefundReference: fmt.Sprintf("onchain:%s:%s:%d", evidence.Chain, evidence.TxID, evidence.Vout), AmountCents: plan.USDAllocation.AmountCents, Currency: "USD", RefundedAt: evidence.VerifiedAt.UTC().Truncate(time.Microsecond), Reason: "permanent_closure", AmountSatoshis: plan.RemainingRefundSatoshis}
	if err = recordServiceRefund(ctx, tx, refund); err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			return nil, ErrServiceRefund
		}
		return nil, err
	}
	raw, err = json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_bitcoin_closure_payouts(review_id,actor_id,chain,txid,vout,block_hash,provider,refund_reference,evidence,top_up_satoshis) VALUES($1,$2,$3,$4,$5,$6,'opennode',$7,$8::jsonb,$9)`, req.ReviewID, actor, evidence.Chain, evidence.TxID, evidence.Vout, evidence.BlockHash, refund.RefundReference, raw, destination.TopUpSatoshis)
	if err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			return nil, ErrServiceRefund
		}
		return nil, err
	}
	out, err := inspectBitcoinClosurePayout(ctx, tx, req.ReviewID)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
