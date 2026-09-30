package getters

import (
	"bytes"
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

type BitcoinClosureDestination struct {
	ReviewID    string                    `json:"review_id"`
	ActorID     string                    `json:"actor_id"`
	Destination bitcoinpayout.Destination `json:"destination"`
	Reason      string                    `json:"reason"`
	CreatedAt   time.Time                 `json:"created_at"`
}

func InspectBitcoinClosureDestination(ctx context.Context, app *config.AppContext, reviewID string) (*BitcoinClosureDestination, error) {
	if id, err := uuid.Parse(reviewID); err != nil || id.String() != reviewID {
		return nil, ErrServiceRefund
	}
	return inspectBitcoinClosureDestination(ctx, app.DB, reviewID)
}

func inspectBitcoinClosureDestination(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, reviewID string) (*BitcoinClosureDestination, error) {
	out := &BitcoinClosureDestination{ReviewID: reviewID}
	err := query.QueryRow(ctx, `SELECT actor_id::text,chain,destination,script_pub_key,reason,created_at FROM service_bitcoin_closure_destinations WHERE review_id=$1`, reviewID).Scan(&out.ActorID, &out.Destination.Chain, &out.Destination.Address, &out.Destination.ScriptPubKey, &out.Reason, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ApproveBitcoinClosureDestination records the authenticated admin's confirmation
// that this address belongs to the refund recipient. The node proves only address
// validity/network, not ownership. Caller identity must come from server auth.
// Approval is immutable and does not reserve funds, authorize sending, or pay.
func ApproveBitcoinClosureDestination(ctx context.Context, app *config.AppContext, node *bitcoinpayout.Client, actor, reviewID, address, reason string) (*BitcoinClosureDestination, error) {
	reason = strings.TrimSpace(reason)
	for _, value := range []string{actor, reviewID} {
		if id, err := uuid.Parse(value); err != nil || id.String() != value {
			return nil, ErrServiceRefund
		}
	}
	if node == nil || reason == "" || len(reason) > 1000 {
		return nil, ErrServiceRefund
	}
	// Check authorization before network I/O, then lock/recheck it before writing.
	var authorized bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account)`, actor).Scan(&authorized); err != nil {
		return nil, err
	}
	if !authorized {
		return nil, ErrServiceRefundAdmin
	}
	destination, err := node.ValidateDestination(ctx, address)
	if err != nil {
		return nil, err
	}
	if destination.Chain != "main" && (app.Env == nil || app.Env.Prod) {
		return nil, ErrServiceRefund
	}
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
	var attempt string
	var closure time.Time
	// Share settlement/refund's order lock. A prior refund must either be visible
	// to recalculation here or wait until this approval commits.
	err = tx.QueryRow(ctx, `SELECT v.attempt_id::text,v.closure_at,v.basis FROM service_bitcoin_closure_reviews v JOIN service_payment_attempts a ON a.id=v.attempt_id JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id WHERE v.id=$1 FOR UPDATE OF o,a`, reviewID).Scan(&attempt, &closure, &raw)
	if err != nil {
		return nil, err
	}
	prior, err := inspectBitcoinClosureDestination(ctx, tx, reviewID)
	if err == nil {
		if prior.ActorID != actor || prior.Reason != reason || prior.Destination != *destination {
			return nil, ErrServiceRefund
		}
		return prior, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var reviewed BitcoinClosurePlan
	if err = json.Unmarshal(raw, &reviewed); err != nil {
		return nil, err
	}
	plan, err := planBitcoinClosureRefund(ctx, tx, attempt, closure)
	if err != nil {
		return nil, err
	}
	before, _ := json.Marshal(reviewed)
	current, _ := json.Marshal(plan)
	if len(plan.ReviewReasons) != 0 || plan.RemainingRefundSatoshis == nil || *plan.RemainingRefundSatoshis <= 0 || !bytes.Equal(before, current) {
		return nil, ErrServiceRefund
	}
	out := &BitcoinClosureDestination{ReviewID: reviewID, ActorID: actor, Destination: *destination, Reason: reason}
	err = tx.QueryRow(ctx, `INSERT INTO service_bitcoin_closure_destinations(review_id,actor_id,chain,destination,script_pub_key,reason) VALUES($1,$2,$3,$4,$5,$6) RETURNING created_at`, reviewID, actor, destination.Chain, destination.Address, destination.ScriptPubKey, reason).Scan(&out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
