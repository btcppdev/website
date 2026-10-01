package getters

import (
	"btcpp-web/internal/config"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

var ErrAccountCancellationAdmin = errors.New("global administrator required for cancellation recovery")

type AccountCancellationStatus struct {
	CheckoutID    string    `json:"checkout_id"`
	ProductCode   string    `json:"product_code"`
	State         string    `json:"state"`
	Attempts      int64     `json:"attempts"`
	RequestedAt   time.Time `json:"requested_at"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	LeaseUntil    time.Time `json:"lease_until"`
	HasSession    bool      `json:"has_session"`
	HasBinding    bool      `json:"has_binding"`
}

func ListAccountDeletionCancellations(ctx context.Context, app *config.AppContext, id string) ([]AccountCancellationStatus, error) {
	rows, err := app.DB.Query(ctx, `SELECT j.checkout_id::text,c.product_code,j.state,j.attempts,j.requested_at,j.next_attempt_at,j.lease_until,c.stripe_session_id IS NOT NULL,EXISTS(SELECT 1 FROM service_stripe_subscriptions s WHERE s.checkout_id=j.checkout_id)
	 FROM account_deletion_cancellations j JOIN service_stripe_checkouts c ON c.id=j.checkout_id
	 WHERE ($1='' AND j.state<>'confirmed') OR j.checkout_id::text=$1 ORDER BY j.requested_at,j.checkout_id LIMIT 100`, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AccountCancellationStatus{}
	for rows.Next() {
		var item AccountCancellationStatus
		if err := rows.Scan(&item.CheckoutID, &item.ProductCode, &item.State, &item.Attempts, &item.RequestedAt, &item.NextAttemptAt, &item.LeaseUntil, &item.HasSession, &item.HasBinding); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// RecoverAccountDeletionCancellation only requeues the inspected review attempt.
// Actor is supplied by trusted operator tooling, never browser form identity.
// Provider identity, cancellation and confirmation checks still run on retry.
func RecoverAccountDeletionCancellation(ctx context.Context, app *config.AppContext, id string, expected int64, actor, reason string) (bool, error) {
	reason = strings.TrimSpace(reason)
	if _, err := uuid.Parse(id); err != nil {
		return false, err
	}
	if _, err := uuid.Parse(actor); err != nil {
		return false, ErrAccountCancellationAdmin
	}
	if expected < 1 || reason == "" || len(reason) > 1000 {
		return false, errors.New("positive attempt and recovery reason required")
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var role int
	err = tx.QueryRow(ctx, `SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id
	 WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account
	 FOR SHARE OF r,p`, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrAccountCancellationAdmin
	}
	if err != nil {
		return false, err
	}
	result, err := tx.Exec(ctx, `WITH recovered AS (
	 UPDATE account_deletion_cancellations j SET state='pending',next_attempt_at=now(),lease_until=now()
	 FROM service_stripe_checkouts c WHERE c.id=j.checkout_id AND c.person_id IS NULL
	 AND j.checkout_id=$1 AND j.state='review' AND j.attempts=$2 AND j.lease_until<=now()
	 AND NOT EXISTS(SELECT 1 FROM people WHERE id=j.person_id)
	 AND EXISTS(SELECT 1 FROM account_deletion_deliveries d WHERE d.person_id=j.person_id)
	 RETURNING j.checkout_id,j.attempts
	) INSERT INTO account_deletion_cancellation_recoveries(checkout_id,after_attempt,actor_id,reason)
	 SELECT checkout_id,attempts,$3,$4 FROM recovered`, id, expected, actor, reason)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}
