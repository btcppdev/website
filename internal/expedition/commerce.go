package expedition

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Fulfillment struct {
	Version       int    `json:"version"`
	ReservationID string `json:"reservation_id"`
	Installment   int    `json:"installment"`
}

// Charge is the product adapter's input to shared commerce. The bridge maps
// these fields to ServiceQuote, with quantity=1, no recurrence and no period.
// Shared commerce adds tax and legal acceptance, creates orders and owns all
// provider calls. Key MUST be used as its purchase idempotency key.
type Charge struct {
	Key, ProductCode, ResourceID, BuyerID, Provider, Currency, Name string
	TotalCents                                                      int64
	ExpiresAt                                                       time.Time
	Fulfillment                                                     json.RawMessage
}

func chargeFor(r Reservation, number int) (Charge, error) {
	if number < 1 || number > len(r.Schedule.Installments) {
		return Charge{}, ErrInvalid
	}
	i := r.Schedule.Installments[number-1]
	raw, err := json.Marshal(Fulfillment{1, r.ID, number})
	if err != nil {
		return Charge{}, err
	}
	expires := time.Date(2028, 1, 24, 0, 0, 0, 0, time.UTC)
	if number == 1 {
		expires = r.HoldExpiresAt
	}
	name := fmt.Sprintf("Antarctica 2028 — %d passenger(s), payment %d/%d", r.Quantity, number, len(r.Schedule.Installments))
	if r.Schedule.BetaTrial {
		name = "Antarctica 2028 — private beta test, one berth"
	}
	return Charge{
		Key: fmt.Sprintf("expedition:%s:%d", r.ID, number), ProductCode: ProductCode,
		ResourceID: r.ID, BuyerID: r.BuyerID, Provider: r.Provider, Currency: "USD",
		Name:       name,
		TotalCents: i.Cents, ExpiresAt: expires, Fulfillment: raw,
	}, nil
}

// Charge returns only the next payable installment, using the persisted fare.
// A confirmed reservation remains occupied even when an installment is overdue;
// cancellation and refunds require an explicit, separately reviewed workflow.
func (s *Store) Charge(ctx context.Context, buyerID, id string, number int) (Charge, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Charge{}, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Charge{}, err
	}
	defer tx.Rollback(ctx)
	charge, err := s.ChargeTx(ctx, tx, buyerID, id, number)
	if err != nil {
		return Charge{}, err
	}
	return charge, tx.Commit(ctx)
}

// ChargeTx rechecks the next installment within the shared order transaction.
func (s *Store) ChargeTx(ctx context.Context, tx pgx.Tx, buyerID, id string, number int) (Charge, error) {
	r, err := scanReservation(tx.QueryRow(ctx, `SELECT `+reservationColumns+` FROM expedition_reservations WHERE id=$1 AND buyer_id=$2 FOR UPDATE`, id, buyerID))
	if err != nil {
		return Charge{}, err
	}
	now, err := s.clock(ctx, tx)
	if err != nil {
		return Charge{}, err
	}
	if r.State == "expired" || (r.State == "held" && !now.Before(r.HoldExpiresAt)) {
		return Charge{}, ErrExpired
	}
	if r.State != "held" && r.State != "confirmed" {
		return Charge{}, ErrReview
	}
	charge, err := chargeFor(r, number)
	if err != nil {
		return Charge{}, err
	}
	if !now.Before(charge.ExpiresAt) {
		return Charge{}, ErrExpired
	}
	var next int
	err = tx.QueryRow(ctx, `SELECT COALESCE(min(number),0) FROM expedition_installments WHERE reservation_id=$1 AND paid_order_id IS NULL`, id).Scan(&next)
	if err != nil {
		return Charge{}, err
	}
	if number != next || now.Before(installmentOpens(r.Schedule.Installments[number-1])) {
		return Charge{}, ErrNotDue
	}
	return charge, nil
}

// PaidChargeReader is implemented by the shared commerce bridge. It MUST read
// durable verified settlement evidence, reject refunded/invalid orders, and
// return the original immutable quote plus the authenticated purchaser identity.
// Never implement this using a browser return URL or a raw provider callback.
type PaidChargeReader interface {
	ReadPaidCharge(context.Context, string) (Charge, error)
}

// Fulfill projects one verified shared order into the expedition contract.
// The commerce dispatcher must acknowledge delivery only after this succeeds.
// ErrReview means the payment was recorded for reconciliation but no berth was
// granted. Replaying that order continues to return ErrReview.
func (s *Store) Fulfill(ctx context.Context, commerce PaidChargeReader, orderID string) error {
	if commerce == nil || orderID == "" {
		return ErrInvalid
	}
	paid, err := commerce.ReadPaidCharge(ctx, orderID)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	err = s.FulfillPaidTx(ctx, tx, paid, orderID)
	if err != nil && err != ErrReview {
		return err
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return commitErr
	}
	return err
}

// FulfillPaidTx must be called only after shared commerce verifies and locks the
// paid order in this transaction. ErrReview must be committed for reconciliation.
func (s *Store) FulfillPaidTx(ctx context.Context, tx pgx.Tx, paid Charge, orderID string) error {
	var f Fulfillment
	if err := json.Unmarshal(paid.Fulfillment, &f); err != nil || f.Version != 1 {
		return ErrInvalid
	}
	if _, err := uuid.Parse(f.ReservationID); err != nil {
		return ErrInvalid
	}
	var err error
	if _, err = tx.Exec(ctx, `SELECT singleton FROM expedition_settings WHERE singleton FOR UPDATE`); err != nil {
		return err
	}
	// Same inventory lock as Reserve: a confirmation cannot race a replacement hold.
	var berth string
	if err = tx.QueryRow(ctx, `SELECT berth FROM expedition_reservations WHERE id=$1`, f.ReservationID).Scan(&berth); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT berth FROM expedition_inventory WHERE berth=$1 FOR UPDATE`, berth); err != nil {
		return err
	}
	r, err := scanReservation(tx.QueryRow(ctx, `SELECT `+reservationColumns+` FROM expedition_reservations WHERE id=$1 FOR UPDATE`, f.ReservationID))
	if err != nil {
		return err
	}
	expected, err := chargeFor(r, f.Installment)
	if err != nil {
		return err
	}
	if !ValidChargeKey(paid.Key, expected.Key, f.Installment) || paid.ProductCode != expected.ProductCode || paid.ResourceID != r.ID || paid.BuyerID != r.BuyerID || paid.Provider != r.Provider || paid.Currency != "USD" || paid.TotalCents != expected.TotalCents || !paid.ExpiresAt.Equal(expected.ExpiresAt) {
		return ErrConflict
	}
	var prior *string
	if err = tx.QueryRow(ctx, `SELECT paid_order_id FROM expedition_installments WHERE reservation_id=$1 AND number=$2`, r.ID, f.Installment).Scan(&prior); err != nil {
		return err
	}
	if prior != nil {
		if *prior != orderID {
			return ErrConflict
		}
		if r.State != "confirmed" {
			return ErrReview
		}
		return nil
	}
	var next int
	if err = tx.QueryRow(ctx, `SELECT min(number) FROM expedition_installments WHERE reservation_id=$1 AND paid_order_id IS NULL`, r.ID).Scan(&next); err != nil {
		return err
	}
	now, err := s.clock(ctx, tx)
	if err != nil {
		return err
	}
	state := "confirmed"
	if (r.State != "held" && r.State != "confirmed") || (r.State == "held" && !now.Before(r.HoldExpiresAt)) || next != f.Installment || now.Before(installmentOpens(r.Schedule.Installments[f.Installment-1])) || !now.Before(expected.ExpiresAt) {
		state = "review"
	}
	if _, err = tx.Exec(ctx, `UPDATE expedition_installments SET paid_order_id=$3 WHERE reservation_id=$1 AND number=$2`, r.ID, f.Installment, orderID); err != nil {
		return err
	}
	if r.State == "abandoned" || r.State == "cancelled" {
		state = r.State
	}
	if _, err = tx.Exec(ctx, `UPDATE expedition_reservations SET state=$2, confirmed_at=CASE WHEN $2='confirmed' THEN COALESCE(confirmed_at,$3) ELSE confirmed_at END WHERE id=$1`, r.ID, state, now); err != nil {
		return err
	}
	if state != "confirmed" {
		return ErrReview
	}
	return nil
}

// Later installment orders may be requoted after expiry without overwriting receipts.
func ValidChargeKey(key, canonical string, number int) bool {
	if key == canonical {
		return true
	}
	if number < 2 || !strings.HasPrefix(key, canonical+":") {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(key, canonical+":"))
	return err == nil
}
func installmentOpens(i Installment) time.Time {
	if i.Number == 1 {
		return i.DueAt
	}
	return i.DueAt.AddDate(0, 0, -14)
}
