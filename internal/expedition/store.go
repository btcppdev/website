package expedition

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	DB  *pgxpool.Pool
	now func() time.Time
}

type Reservation struct {
	ID, BuyerID, Berth, Plan, Provider, State string
	CabinID, Sharing                          string
	Quantity                                  int
	CreatedAt, HoldExpiresAt                  time.Time
	Schedule                                  Schedule
}

func (s *Store) clock(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	if s.now != nil {
		return s.now().UTC(), nil
	}
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, err
}

const reservationColumns = `id::text,buyer_id,berth,plan,provider,state,created_at,hold_expires_at,schedule,COALESCE(cabin_id,''),COALESCE(sharing,''),quantity`

func scanReservation(row pgx.Row) (Reservation, error) {
	var r Reservation
	var raw []byte
	err := row.Scan(&r.ID, &r.BuyerID, &r.Berth, &r.Plan, &r.Provider, &r.State, &r.CreatedAt, &r.HoldExpiresAt, &raw, &r.CabinID, &r.Sharing, &r.Quantity)
	if err == nil {
		err = json.Unmarshal(raw, &r.Schedule)
	}
	return r, err
}

// Reserve holds exactly one berth. The caller supplies the authenticated shared
// customer identity, never an identity or price accepted from form fields.
// Exact retries return the original reservation, including expired reservations.
func (s *Store) Reserve(ctx context.Context, buyerID, key, berth, plan, provider string) (Reservation, error) {
	return s.ReserveGroup(ctx, buyerID, key, berth, plan, provider, "coed", 1)
}

// ReserveGroup holds all requested berths in one cabin atomically.
// Sharing is an explicit cabin preference, not an inferred passenger attribute.
// The checkout must ask for it; the single-berth Reserve helper selects co-ed.
func (s *Store) ReserveGroup(ctx context.Context, buyerID, key, berth, plan, provider, sharing string, quantity int) (Reservation, error) {
	var empty Reservation
	if strings.TrimSpace(buyerID) == "" || len(key) < 16 || len(key) > 128 || (sharing != "male" && sharing != "female" && sharing != "coed") || quantity < 1 || quantity > 4 {
		return empty, ErrInvalid
	}
	// Roommate requests are relevant only to an unfilled twin cabin. Triple and
	// quad cabins are co-ed; filling a cabin needs no outside roommate preference.
	if berth != "shared" || quantity == 2 {
		sharing = "coed"
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	var staff int
	if err = tx.QueryRow(ctx, `SELECT staff_berths FROM expedition_settings WHERE singleton FOR UPDATE`).Scan(&staff); err != nil {
		return empty, err
	}
	// Serialize the idempotency key even when competing requests select different berths.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, buyerID+":"+key); err != nil {
		return empty, err
	}
	r, err := scanReservation(tx.QueryRow(ctx, `SELECT `+reservationColumns+` FROM expedition_reservations WHERE buyer_id=$1 AND request_key=$2`, buyerID, key))
	if err == nil {
		if r.Berth != berth || r.Plan != plan || r.Provider != provider || r.Sharing != sharing || r.Quantity != quantity {
			return empty, ErrConflict
		}
		return r, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var capacity, holdSeconds int
	var open bool
	var closes time.Time
	err = tx.QueryRow(ctx, `SELECT capacity,sales_open,booking_closes_at,hold_seconds FROM expedition_inventory WHERE berth=$1 FOR UPDATE`, berth).Scan(&capacity, &open, &closes, &holdSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrClosed
	}
	if err != nil {
		return empty, err
	}
	now, err := s.clock(ctx, tx)
	if err != nil {
		return empty, err
	}
	if !open || !now.Before(closes) {
		return empty, ErrClosed
	}
	availability, err := s.availability(ctx, tx, staff)
	if err != nil {
		return empty, err
	}
	if quantity > availability.Remaining {
		return empty, ErrSoldOut
	}
	schedule, err := NewGroupSchedule(berth, plan, provider, quantity, now)
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `UPDATE expedition_reservations SET state='expired' WHERE berth=$1 AND state='held' AND hold_expires_at <= $2`, berth, now); err != nil {
		return empty, err
	}
	var used int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity),0) FROM expedition_reservations WHERE berth=$1 AND (state='held' OR confirmed_at IS NOT NULL)`, berth).Scan(&used); err != nil {
		return empty, err
	}
	if used+quantity > capacity {
		return empty, ErrSoldOut
	}
	// Category inventory lock also serializes cabin assignment and fulfillment.
	// Prefer already occupied compatible cabins to preserve empty cabins for groups.
	var cabinID string
	err = tx.QueryRow(ctx, `SELECT c.id FROM expedition_cabins c
	 LEFT JOIN LATERAL (
	   SELECT COALESCE(sum(r.quantity),0) used,
	     COALESCE(bool_and(r.sharing=$3),true) compatible
	   FROM expedition_reservations r WHERE r.cabin_id=c.id
	     AND (r.state='held' OR r.confirmed_at IS NOT NULL)
	 ) occupancy ON true
	 WHERE c.berth=$1 AND c.enabled
	   AND occupancy.used+$2 <= c.capacity
	 ORDER BY occupancy.compatible DESC,occupancy.used DESC,c.id LIMIT 1`, berth, quantity, sharing).Scan(&cabinID)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrSoldOut
	}
	if err != nil {
		return empty, err
	}
	expires := now.Add(time.Duration(holdSeconds) * time.Second)
	if closes.Before(expires) {
		expires = closes
	}
	r = Reservation{ID: uuid.NewString(), BuyerID: buyerID, Berth: berth, Plan: plan, Provider: provider, State: "held", CreatedAt: now, HoldExpiresAt: expires, Schedule: schedule}
	r.CabinID, r.Sharing, r.Quantity = cabinID, sharing, quantity
	raw, err := json.Marshal(schedule)
	if err != nil {
		return empty, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO expedition_reservations(id,buyer_id,request_key,berth,plan,provider,state,created_at,hold_expires_at,schedule,cabin_id,sharing,quantity) VALUES($1,$2,$3,$4,$5,$6,'held',$7,$8,$9,$10,$11,$12)`, r.ID, buyerID, key, berth, plan, provider, now, expires, raw, cabinID, sharing, quantity)
	if err != nil {
		return empty, err
	}
	for _, i := range schedule.Installments {
		if _, err = tx.Exec(ctx, `INSERT INTO expedition_installments(reservation_id,number,due_at,cents) VALUES($1,$2,$3,$4)`, r.ID, i.Number, i.DueAt, i.Cents); err != nil {
			return empty, err
		}
	}
	return r, tx.Commit(ctx)
}
