package getters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/jackc/pgx/v5"
)

var ErrExpeditionCancellation = errors.New("booking changed or payments need review; refresh before cancelling")

type ExpeditionCancellation struct {
	ID, Buyer, State, Provider, PolicyVersion, Terms, Token string
	Quantity                                                int
	Paid, Refunded, Deposit, Entitlement, RefundDue         int64
	Eligible, Blocked                                       bool
	CancelledAt                                             *time.Time
	Actor, Reason                                           string
	Orders                                                  []ExpeditionCancellationOrder
}
type ExpeditionCancellationOrder struct {
	PublicID, Provider, Reference string
	Paid, Refunded                int64
}

func ExpeditionCancellationPreview(ctx context.Context, app *config.AppContext, id string) (*ExpeditionCancellation, error) {
	tx, err := app.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	c, err := expeditionCancellationSnapshot(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return c, tx.Commit(ctx)
}

func expeditionCancellationSnapshot(ctx context.Context, tx pgx.Tx, id string) (*ExpeditionCancellation, error) {
	c := &ExpeditionCancellation{ID: id}
	var raw []byte
	var schedule expedition.Schedule
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT buyer_id,state,provider,quantity,schedule,now() FROM expedition_reservations WHERE id=$1`, id).Scan(&c.Buyer, &c.State, &c.Provider, &c.Quantity, &raw, &now)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &schedule); err != nil {
		return nil, err
	}
	c.Deposit = expeditionNonrefundableDeposit(schedule)
	c.Eligible, err = expeditionCancellationBeforeCutoff(now)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT coalesce(p.quote_snapshot->'legal_acceptance'->>'version',''),coalesce(p.quote_snapshot->'legal_acceptance'->>'terms_text','') FROM service_purchase_requests p WHERE p.request_key='expedition:'||$1||':1' AND p.person_id::text=$2`, id, c.Buyer).Scan(&c.PolicyVersion, &c.Terms)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT o.public_id,o.payment_provider,coalesce(o.payment_provider_id,''),o.total_cents,
 coalesce((SELECT sum(f.amount_cents) FROM service_payment_refunds f JOIN service_payment_attempts a ON a.id=f.attempt_id WHERE a.purchase_id=p.id),0)
 FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id
 WHERE p.quote_snapshot->>'product_code'=$1 AND p.quote_snapshot->>'resource_id'=$2
 AND EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=p.id AND a.settled_at IS NOT NULL)
 ORDER BY o.public_id`, expedition.ProductCode, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var o ExpeditionCancellationOrder
		if err = rows.Scan(&o.PublicID, &o.Provider, &o.Reference, &o.Paid, &o.Refunded); err != nil {
			rows.Close()
			return nil, err
		}
		c.Orders = append(c.Orders, o)
		c.Paid += o.Paid
		c.Refunded += o.Refunded
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if c.Eligible {
		c.Entitlement = max(int64(0), c.Paid-c.Deposit)
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id
 WHERE p.quote_snapshot->>'product_code'=$1 AND p.quote_snapshot->>'resource_id'=$2 AND
 (EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=p.id AND a.settled_at IS NULL)
 OR (o.status IN ('paid','partially_refunded','refunded') AND NOT EXISTS(SELECT 1 FROM expedition_installments i WHERE i.reservation_id=$2::uuid AND i.paid_order_id=o.id::text))))`, expedition.ProductCode, id).Scan(&c.Blocked)
	if err != nil {
		return nil, err
	}
	// Unknown/custom accepted cancellation clauses must be reviewed separately.
	c.Blocked = c.Blocked || !strings.Contains(c.Terms, "less the initial 20% deposit") || !strings.Contains(c.Terms, "October 1, 2027")
	var saved []byte
	err = tx.QueryRow(ctx, `SELECT cancelled_at,actor_id::text,reason,snapshot FROM expedition_cancellations WHERE reservation_id=$1`, id).Scan(&c.CancelledAt, &c.Actor, &c.Reason, &saved)
	if err == nil {
		var original ExpeditionCancellation
		if err = json.Unmarshal(saved, &original); err != nil {
			return nil, err
		}
		c.Entitlement = original.Entitlement
		c.Deposit = original.Deposit
		c.Eligible = original.Eligible
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	c.RefundDue = max(int64(0), c.Entitlement-c.Refunded)
	raw, err = json.Marshal(c)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	c.Token = hex.EncodeToString(sum[:])
	return c, nil
}

// CancelExpeditionBooking never sends money or marks an unperformed refund as paid.
// Locks match payment creation/settlement and lifecycle processing. A repeat POST
// returns the original cancellation without releasing inventory a second time.
func CancelExpeditionBooking(ctx context.Context, app *config.AppContext, id, actor, token, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) == 0 || len(reason) > 1000 {
		return ErrExpeditionCancellation
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var buyer string
	if err = tx.QueryRow(ctx, `SELECT buyer_id FROM expedition_reservations WHERE id=$1`, id).Scan(&buyer); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+buyer); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT o.id FROM shop_orders o JOIN service_purchase_requests p ON p.order_id=o.id WHERE p.quote_snapshot->>'product_code'=$1 AND p.quote_snapshot->>'resource_id'=$2 ORDER BY o.id FOR UPDATE OF o`, expedition.ProductCode, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT singleton FROM expedition_settings WHERE singleton FOR UPDATE`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM expedition_reservations WHERE id=$1 FOR UPDATE`, id); err != nil {
		return err
	}
	c, err := expeditionCancellationSnapshot(ctx, tx, id)
	if err != nil {
		return err
	}
	if c.State == "cancelled" && c.CancelledAt != nil {
		return tx.Commit(ctx)
	}
	if c.State != "confirmed" || c.Blocked || c.Token != token {
		return ErrExpeditionCancellation
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO expedition_cancellations(reservation_id,actor_id,reason,snapshot) VALUES($1,$2,$3,$4)`, id, actor, reason, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE expedition_reservations SET state='cancelled',released_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE shop_orders o SET status='cancelled' FROM service_purchase_requests p WHERE p.order_id=o.id AND p.quote_snapshot->>'product_code'=$1 AND p.quote_snapshot->>'resource_id'=$2 AND o.status='pending'`, expedition.ProductCode, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE expedition_lifecycle_mail m SET state='suppressed' FROM expedition_lifecycle_events e WHERE e.id=m.event_id AND e.reservation_id=$1 AND e.kind='reminder' AND m.state='pending'`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func expeditionCancellationBeforeCutoff(now time.Time) (bool, error) {
	zone, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return false, err
	}
	return now.Before(time.Date(2027, 10, 1, 0, 0, 0, 0, zone)), nil
}

// Keep the original five-payment deposit rounding; shorter plans still retain
// only 20% of the fare, not the larger first payment.
func expeditionNonrefundableDeposit(schedule expedition.Schedule) int64 {
	if schedule.Plan == expedition.Installments && len(schedule.Installments) == 5 {
		return schedule.Installments[0].Cents
	}
	return (schedule.TotalCents + 4) / 5
}
