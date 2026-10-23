package expedition

import (
	"context"
	"time"
)

type DueInstallment struct {
	ReservationID, BuyerID string
	Number                 int
	DueAt                  time.Time
	Cents                  int64
}

// Due returns the earliest unpaid installment of each confirmed booking.
// It is read-only: it neither charges a card nor sends email. A future shared
// reminder worker must deduplicate delivery and call Charge before checkout.
// Paid, held and review bookings cannot generate extra/recurring installments.
func (s *Store) Due(ctx context.Context, limit int) ([]DueInstallment, error) {
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	now, err := s.clock(ctx, tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.buyer_id,i.number,i.due_at,i.cents
		FROM expedition_reservations r
		JOIN LATERAL (SELECT number,due_at,cents FROM expedition_installments
		  WHERE reservation_id=r.id AND paid_order_id IS NULL ORDER BY number LIMIT 1) i ON true
		WHERE r.state='confirmed' AND i.due_at <= $1
		ORDER BY i.due_at,r.id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DueInstallment
	for rows.Next() {
		var d DueInstallment
		if err = rows.Scan(&d.ReservationID, &d.BuyerID, &d.Number, &d.DueAt, &d.Cents); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}
