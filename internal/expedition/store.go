package expedition

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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

// ReserveGroup is the single-cabin convenience wrapper around atomic party booking.
func (s *Store) ReserveGroup(ctx context.Context, buyerID, key, berth, plan, provider, sharing string, quantity int) (Reservation, error) {
	return s.ReserveParty(ctx, buyerID, key, plan, provider, quantity, []CabinSelection{{Berth: berth, Quantity: quantity, Preference: sharing}})
}

// ReserveParty holds every selected arrangement or none. All category/cabin
// allocation and staff-reserve checks share the expedition-wide settings lock.
func (s *Store) ReserveParty(ctx context.Context, buyerID, key, plan, provider string, spots int, selections []CabinSelection) (Reservation, error) {
	return s.reserveParty(ctx, buyerID, key, plan, provider, spots, selections, false)
}

// ReserveBetaParty is available only to the verified, fixed test identity. The
// price is fixed server-side; no request can choose its own discount.
func (s *Store) ReserveBetaParty(ctx context.Context, buyerID, key string, selections []CabinSelection) (Reservation, error) {
	return s.ReserveBetaPartyWithProvider(ctx, buyerID, key, Bitcoin, selections)
}

func (s *Store) ReserveBetaPartyWithProvider(ctx context.Context, buyerID, key, provider string, selections []CabinSelection) (Reservation, error) {
	return s.reserveParty(ctx, buyerID, key, Full, provider, 1, selections, true)
}
func (s *Store) reserveParty(ctx context.Context, buyerID, key, plan, provider string, spots int, selections []CabinSelection, beta bool) (Reservation, error) {
	var empty Reservation
	if strings.TrimSpace(buyerID) == "" || len(key) < 16 || len(key) > 128 {
		return empty, ErrInvalid
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
	now, err := s.clock(ctx, tx)
	if err != nil {
		return empty, err
	}
	if beta {
		var eligible bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM person_emails e JOIN people p ON p.id=e.person_id WHERE e.person_id::text=$1 AND lower(e.email::text)='nifty@btcpp.dev' AND e.verified_at IS NOT NULL AND NOT p.is_deleted_account)`, buyerID).Scan(&eligible); err != nil {
			return empty, err
		}
		if !eligible || spots != 1 || plan != Full || (provider != Bitcoin && provider != Stripe) {
			return empty, ErrInvalid
		}
	}
	// Validate identity on retries using the original contract date, so expired
	// contracts still replay after the installment enrollment cutoff.
	r, err := scanReservation(tx.QueryRow(ctx, `SELECT `+reservationColumns+` FROM expedition_reservations WHERE buyer_id=$1 AND request_key=$2`, buyerID, key))
	if err == nil {
		requested, e := QuoteParty(spots, selections, plan, provider, r.CreatedAt)
		existing := r.Schedule.Selections
		if len(existing) == 0 {
			existing = []CabinSelection{{r.Berth, r.Quantity, r.Sharing}}
		}
		if r.Schedule.BetaTrial != beta || e != nil || r.Plan != plan || r.Provider != provider || r.Quantity != spots || !reflect.DeepEqual(existing, requested.Selections) {
			return empty, ErrConflict
		}
		return r, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	quote, err := QuoteParty(spots, selections, plan, provider, now)
	if err != nil {
		return empty, err
	}
	if beta {
		var claimed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expedition_beta_claims c JOIN expedition_reservations r ON r.id=c.reservation_id WHERE r.state<>'cancelled' OR r.released_at IS NULL)`).Scan(&claimed); err != nil {
			return empty, err
		}
		if claimed {
			return empty, ErrConflict
		}
		quote.TotalCents = BetaFareCents(provider)
		quote.Installments = []Installment{{Number: 1, DueAt: now, Cents: quote.TotalCents}}
	}
	available, err := s.availability(ctx, tx, staff)
	if err != nil {
		return empty, err
	}
	if spots > available.Remaining {
		return empty, ErrSoldOut
	}
	if _, err = tx.Exec(ctx, `UPDATE expedition_reservations SET state='expired' WHERE state='held' AND hold_expires_at<=$1`, now); err != nil {
		return empty, err
	}
	type allocation struct {
		Cabin     string
		Selection CabinSelection
	}
	var assigned []allocation
	usedCabins := []string{}
	expires := now.Add(24 * time.Hour)
	for _, a := range quote.Selections {
		var capacity, seconds int
		var open bool
		var closes time.Time
		err = tx.QueryRow(ctx, `SELECT capacity,sales_open,booking_closes_at,hold_seconds FROM expedition_inventory WHERE berth=$1 FOR UPDATE`, a.Berth).Scan(&capacity, &open, &closes, &seconds)
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrClosed
		}
		if err != nil {
			return empty, err
		}
		if !open || !now.Before(closes) {
			return empty, ErrClosed
		}
		var used int
		err = tx.QueryRow(ctx, `SELECT COALESCE(sum(a.quantity),0) FROM expedition_allocations a JOIN expedition_reservations r ON r.id=a.reservation_id WHERE a.berth=$1 AND (r.state='held' OR (r.confirmed_at IS NOT NULL AND r.released_at IS NULL))`, a.Berth).Scan(&used)
		if err != nil {
			return empty, err
		}
		for _, prior := range assigned {
			if prior.Selection.Berth == a.Berth {
				used += prior.Selection.Quantity
			}
		}
		if used+a.Quantity > capacity {
			return empty, ErrSoldOut
		}
		var cabin string
		err = tx.QueryRow(ctx, `SELECT c.id FROM expedition_cabins c
   LEFT JOIN LATERAL (SELECT COALESCE(sum(a.quantity),0) used,COALESCE(bool_and(a.preference=$3),true) compatible
     FROM expedition_allocations a JOIN expedition_reservations r ON r.id=a.reservation_id
     WHERE a.cabin_id=c.id AND (r.state='held' OR (r.confirmed_at IS NOT NULL AND r.released_at IS NULL))) occupancy ON true
   WHERE c.berth=$1 AND c.enabled AND occupancy.used+$2<=c.capacity AND NOT(c.id=ANY($4::text[]))
   ORDER BY occupancy.compatible DESC,occupancy.used DESC,c.id LIMIT 1`, a.Berth, a.Quantity, a.Preference, usedCabins).Scan(&cabin)
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrSoldOut
		}
		if err != nil {
			return empty, err
		}
		assigned = append(assigned, allocation{cabin, a})
		usedCabins = append(usedCabins, cabin)
		end := now.Add(time.Duration(seconds) * time.Second)
		if closes.Before(end) {
			end = closes
		}
		if end.Before(expires) {
			expires = end
		}
	}
	var standard int64
	for _, a := range quote.Selections {
		one, _ := NewGroupSchedule(a.Berth, Full, Stripe, a.Quantity, now)
		standard += one.StandardCents
	}
	first := assigned[0]
	schedule := Schedule{BetaTrial: beta, Version: 2, Quantity: spots, Berth: first.Selection.Berth, Plan: plan, Provider: provider, Currency: "USD", StandardCents: standard, TotalCents: quote.TotalCents, Installments: quote.Installments, Selections: quote.Selections}
	if beta {
		schedule.StandardCents = quote.TotalCents
	}
	r = Reservation{ID: uuid.NewString(), BuyerID: buyerID, Berth: first.Selection.Berth, Plan: plan, Provider: provider, State: "held", CreatedAt: now, HoldExpiresAt: expires, Schedule: schedule, CabinID: first.Cabin, Sharing: first.Selection.Preference, Quantity: spots}
	raw, err := json.Marshal(schedule)
	if err != nil {
		return empty, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO expedition_reservations(id,buyer_id,request_key,berth,plan,provider,state,created_at,hold_expires_at,schedule,cabin_id,sharing,quantity) VALUES($1,$2,$3,$4,$5,$6,'held',$7,$8,$9,$10,$11,$12)`, r.ID, buyerID, key, r.Berth, plan, provider, now, expires, raw, r.CabinID, r.Sharing, spots)
	if err != nil {
		return empty, err
	}
	if beta {
		if claim, err := tx.Exec(ctx, `INSERT INTO expedition_beta_claims(email,buyer_id,reservation_id) VALUES('nifty@btcpp.dev',$1,$2)
 ON CONFLICT(email) DO UPDATE SET buyer_id=excluded.buyer_id,reservation_id=excluded.reservation_id,created_at=now()
 WHERE EXISTS(SELECT 1 FROM expedition_reservations old WHERE old.id=expedition_beta_claims.reservation_id AND old.state='cancelled' AND old.released_at IS NOT NULL)`, buyerID, r.ID); err != nil {
			return empty, err
		} else if claim.RowsAffected() != 1 {
			return empty, ErrConflict
		}
	}
	for _, a := range assigned {
		if _, err = tx.Exec(ctx, `INSERT INTO expedition_allocations(reservation_id,cabin_id,berth,quantity,preference) VALUES($1,$2,$3,$4,$5)`, r.ID, a.Cabin, a.Selection.Berth, a.Selection.Quantity, a.Selection.Preference); err != nil {
			return empty, err
		}
	}
	for _, i := range schedule.Installments {
		if _, err = tx.Exec(ctx, `INSERT INTO expedition_installments(reservation_id,number,due_at,cents) VALUES($1,$2,$3,$4)`, r.ID, i.Number, i.DueAt, i.Cents); err != nil {
			return empty, err
		}
	}
	return r, tx.Commit(ctx)
}
