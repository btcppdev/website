package expedition

import (
	"context"
	"github.com/jackc/pgx/v5"
)

const DefaultStaffBerths = 3
const TotalBerths = 25

type Availability struct{ Total, Staff, Remaining int }

func (s *Store) availability(ctx context.Context, tx pgx.Tx, staff int) (Availability, error) {
	a := Availability{Staff: staff}
	now, err := s.clock(ctx, tx)
	if err != nil {
		return a, err
	}
	var used int
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(capacity),0) FROM expedition_cabins WHERE enabled`).Scan(&a.Total)
	if err != nil {
		return a, err
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity),0) FROM expedition_reservations
	 WHERE confirmed_at IS NOT NULL OR (state='held' AND hold_expires_at>$1)`, now).Scan(&used)
	a.Remaining = max(0, a.Total-staff-used)
	return a, err
}

// Staff reserves are global, not an arbitrary assignment of a cabin category.
// All booking transactions lock this row before category inventory locks.
func (s *Store) Availability(ctx context.Context) (Availability, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Availability{}, err
	}
	defer tx.Rollback(ctx)
	var staff int
	if err = tx.QueryRow(ctx, `SELECT staff_berths FROM expedition_settings WHERE singleton FOR UPDATE`).Scan(&staff); err != nil {
		return Availability{}, err
	}
	a, err := s.availability(ctx, tx, staff)
	if err != nil {
		return a, err
	}
	return a, tx.Commit(ctx)
}

// SetStaffBerths is an operator-only operation. The caller must authorize the
// administrator. Increasing the reserve cannot displace holds or paid bookings.
func (s *Store) SetStaffBerths(ctx context.Context, count int) error {
	if count < 0 || count > TotalBerths {
		return ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var prior int
	if err = tx.QueryRow(ctx, `SELECT staff_berths FROM expedition_settings WHERE singleton FOR UPDATE`).Scan(&prior); err != nil {
		return err
	}
	a, err := s.availability(ctx, tx, 0)
	if err != nil {
		return err
	}
	if count > a.Remaining {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE expedition_settings SET staff_berths=$1 WHERE singleton`, count); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
