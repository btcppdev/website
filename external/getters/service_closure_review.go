package getters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BitcoinClosureReview struct {
	ID        string              `json:"id"`
	ActorID   string              `json:"actor_id"`
	Reason    string              `json:"reason"`
	Basis     *BitcoinClosurePlan `json:"basis"`
	CreatedAt time.Time           `json:"created_at"`
}

// ReviewBitcoinClosureRefund freezes the amount the authenticated global admin
// actually reviewed, calculated again under the settlement/refund order lock.
// It does not reserve funds, initiate a payout, or record a verified refund.
// A later executor must revalidate the basis before authorizing payment.
func ReviewBitcoinClosureRefund(ctx context.Context, app *config.AppContext, actor, reviewID, attemptID string, closure time.Time, expectedSatoshis int64, reason string) (*BitcoinClosureReview, error) {
	reason = strings.TrimSpace(reason)
	for _, value := range []string{actor, reviewID, attemptID} {
		if id, err := uuid.Parse(value); err != nil || id.String() != value {
			return nil, ErrServiceRefund
		}
	}
	if expectedSatoshis <= 0 || expectedSatoshis > 2100000000000000 || reason == "" || len(reason) > 1000 || closure.IsZero() || closure.Nanosecond()%1000 != 0 {
		return nil, ErrServiceRefund
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var authorized int
	err = tx.QueryRow(ctx, `SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account FOR SHARE OF r,p`, actor).Scan(&authorized)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServiceRefundAdmin
	}
	if err != nil {
		return nil, err
	}
	var order string
	err = tx.QueryRow(ctx, `SELECT o.id::text FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id WHERE a.id=$1 FOR UPDATE OF o,a`, attemptID).Scan(&order)
	if err != nil {
		return nil, err
	}
	review := &BitcoinClosureReview{ID: reviewID, ActorID: actor, Reason: reason}
	var raw []byte
	var matches bool
	err = tx.QueryRow(ctx, `SELECT actor_id=$2 AND attempt_id=$3 AND closure_at=$4 AND amount_satoshis=$5 AND reason=$6,basis,created_at FROM service_bitcoin_closure_reviews WHERE id=$1`, reviewID, actor, attemptID, closure.UTC(), expectedSatoshis, reason).Scan(&matches, &raw, &review.CreatedAt)
	if err == nil {
		if !matches {
			return nil, ErrServiceRefund
		}
		if err = json.Unmarshal(raw, &review.Basis); err != nil {
			return nil, err
		}
		return review, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	plan, err := planBitcoinClosureRefund(ctx, tx, attemptID, closure)
	if err != nil {
		return nil, err
	}
	if len(plan.ReviewReasons) != 0 || plan.RemainingRefundSatoshis == nil || *plan.RemainingRefundSatoshis != expectedSatoshis {
		return nil, ErrServiceRefund
	}
	raw, err = json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO service_bitcoin_closure_reviews(id,attempt_id,actor_id,closure_at,amount_satoshis,basis,reason) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7) RETURNING created_at`, reviewID, attemptID, actor, closure.UTC(), expectedSatoshis, raw, reason).Scan(&review.CreatedAt); err != nil {
		return nil, err
	}
	review.Basis = plan
	return review, tx.Commit(ctx)
}
