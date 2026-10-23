package expedition

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Uses a unique disposable schema, never existing tables. Explicit opt-in URL.
func testStore(t *testing.T, capacity int) (*Store, *time.Time) {
	t.Helper()
	url := os.Getenv("EXPEDITION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set EXPEDITION_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "expedition_test_" + uuid.NewString()[:8]
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	sql, err := os.ReadFile("../../db/migrations/132_expedition_reservations.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	sql, err = os.ReadFile("../../db/migrations/133_expedition_cabins.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	sql, err = os.ReadFile("../../db/migrations/134_expedition_staff_reserve.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	sql, err = os.ReadFile("../../db/migrations/135_expedition_party_allocations.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO expedition_inventory(berth,capacity,sales_open,booking_closes_at,hold_seconds) VALUES('shared',$1,true,'2028-01-01',900),('quad',$1,false,'2028-01-01',900)`, capacity)
	if err != nil {
		t.Fatal(err)
	}
	// Existing cabin tests isolate their own capacity; staff reserves are covered separately.
	var staff int
	if err = pool.QueryRow(ctx, `SELECT staff_berths FROM expedition_settings`).Scan(&staff); err != nil || staff != 3 {
		t.Fatal("default staff reserve", staff, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE expedition_settings SET staff_berths=0`); err != nil {
		t.Fatal(err)
	}
	now := bookingTime
	return &Store{DB: pool, now: func() time.Time { return now }}, &now
}

type paidReader struct {
	charge Charge
	err    error
}

func (p paidReader) ReadPaidCharge(context.Context, string) (Charge, error) { return p.charge, p.err }

func reserve(t *testing.T, s *Store, plan string) Reservation {
	t.Helper()
	r, err := s.Reserve(context.Background(), "buyer", uuid.NewString(), "shared", plan, Bitcoin)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReservationConcurrency(t *testing.T) {
	s, _ := testStore(t, 1)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Reserve(ctx, "buyer", uuid.NewString(), "shared", Full, Bitcoin)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrSoldOut) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("sold %d berths, capacity 1", success)
	}
}

func TestReservationReplayAndExpiry(t *testing.T) {
	s, now := testStore(t, 1)
	ctx := context.Background()
	key := uuid.NewString()
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Reserve(ctx, "buyer", key, "shared", Full, Bitcoin)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate reservation")
		}
		id = got
	}
	if _, err := s.Reserve(ctx, "buyer", key, "quad", Full, Bitcoin); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, "buyer", uuid.NewString(), "quad", Full, Bitcoin); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	charge, err := s.Charge(ctx, "buyer", id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Charge(ctx, "someone-else", id, 1); err == nil {
		t.Fatal("buyer isolation failed")
	}
	*now = now.Add(15 * time.Minute)
	if _, err = s.Charge(ctx, "buyer", id, 1); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
	replacement := reserve(t, s, Full)
	if replacement.ID == id {
		t.Fatal("reused expired reservation")
	}
	for n := 0; n < 2; n++ {
		if err = s.Fulfill(ctx, paidReader{charge: charge}, "late-order"); !errors.Is(err, ErrReview) {
			t.Fatal(err)
		}
	}
	var state string
	if err = s.DB.QueryRow(ctx, `SELECT state FROM expedition_reservations WHERE id=$1`, id).Scan(&state); err != nil || state != "review" {
		t.Fatal(state, err)
	}
	replayed, err := s.Reserve(ctx, "buyer", key, "shared", Full, Bitcoin)
	if err != nil || replayed.ID != id || replayed.State != "review" {
		t.Fatal(replayed, err)
	}
}

func TestFinitePayments(t *testing.T) {
	s, now := testStore(t, 1)
	ctx := context.Background()
	r := reserve(t, s, Installments)
	for n, i := range r.Schedule.Installments {
		if n > 0 {
			if _, err := s.Charge(ctx, r.BuyerID, r.ID, i.Number); !errors.Is(err, ErrNotDue) {
				t.Fatal("early installment", err)
			}
		}
		*now = i.DueAt
		due, err := s.Due(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 && len(due) != 0 {
			t.Fatal("unconfirmed booking queued")
		}
		if n > 0 && (len(due) != 1 || due[0].Number != i.Number) {
			t.Fatal("wrong due installment", due)
		}
		charge, err := s.Charge(ctx, r.BuyerID, r.ID, i.Number)
		if err != nil {
			t.Fatal(err)
		}
		order := fmt.Sprintf("order-%d", i.Number)
		bad := charge
		bad.TotalCents--
		if err = s.Fulfill(ctx, paidReader{charge: bad}, order); !errors.Is(err, ErrConflict) {
			t.Fatal("wrong amount accepted", err)
		}
		if err = s.Fulfill(ctx, paidReader{err: errors.New("unverified")}, order); err == nil {
			t.Fatal("unverified payment accepted")
		}
		for retry := 0; retry < 2; retry++ {
			if err = s.Fulfill(ctx, paidReader{charge: charge}, order); err != nil {
				t.Fatal(err)
			}
		}
		if err = s.Fulfill(ctx, paidReader{charge: charge}, order+"-duplicate"); !errors.Is(err, ErrConflict) {
			t.Fatal("duplicate order accepted", err)
		}
		if _, err = s.Charge(ctx, r.BuyerID, r.ID, i.Number); !errors.Is(err, ErrNotDue) {
			t.Fatal("paid installment payable", err)
		}
	}
	if _, err := s.Charge(ctx, r.BuyerID, r.ID, 6); !errors.Is(err, ErrInvalid) {
		t.Fatal("sixth installment", err)
	}
	if due, err := s.Due(ctx, 100); err != nil || len(due) != 0 {
		t.Fatal("paid schedule still due", due, err)
	}
	var paid int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM expedition_installments WHERE reservation_id=$1 AND paid_order_id IS NOT NULL`, r.ID).Scan(&paid); err != nil || paid != 5 {
		t.Fatal(paid, err)
	}
	if _, err := s.Reserve(ctx, "other", uuid.NewString(), "shared", Full, Stripe); !errors.Is(err, ErrSoldOut) {
		t.Fatal("confirmed berth released", err)
	}
}

func TestConfirmedReviewRetainsBerth(t *testing.T) {
	s, now := testStore(t, 1)
	ctx := context.Background()
	r := reserve(t, s, Installments)
	c, err := s.Charge(ctx, r.BuyerID, r.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, paidReader{charge: c}, "deposit"); err != nil {
		t.Fatal(err)
	}
	// A malformed/out-of-sequence shared order must not free a confirmed berth.
	c, err = chargeFor(r, 3)
	if err != nil {
		t.Fatal(err)
	}
	*now = r.Schedule.Installments[2].DueAt
	if err = s.Fulfill(ctx, paidReader{charge: c}, "out-of-sequence"); !errors.Is(err, ErrReview) {
		t.Fatal(err)
	}
	if _, err = s.Reserve(ctx, "other", uuid.NewString(), "shared", Full, Stripe); !errors.Is(err, ErrSoldOut) {
		t.Fatal("review freed confirmed berth", err)
	}
}
