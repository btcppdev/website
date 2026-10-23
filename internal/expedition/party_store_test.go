package expedition

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sync"
	"testing"
)

func TestPartyReservationAtomic(t *testing.T) {
	s, _ := testStore(t, 18)
	ctx := context.Background()
	selection := []CabinSelection{{"shared", 2, "coed"}, {"quad", 1, "coed"}}
	if _, err := s.ReserveParty(ctx, "buyer", uuid.NewString(), Full, Stripe, 3, selection); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	var count int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM expedition_reservations`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partially held party", count, err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE expedition_inventory SET sales_open=true WHERE berth='quad'`); err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	r, err := s.ReserveParty(ctx, "buyer", key, Full, Stripe, 3, selection)
	if err != nil {
		t.Fatal(err)
	}
	if r.Quantity != 3 || r.Schedule.TotalCents != 4830000 {
		t.Fatal(r)
	}
	retry, err := s.ReserveParty(ctx, "buyer", key, Full, Stripe, 3, selection)
	if err != nil || retry.ID != r.ID {
		t.Fatal(retry, err)
	}
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM expedition_allocations WHERE reservation_id=$1`, r.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	changed := []CabinSelection{{"shared", 1, "coed"}, {"quad", 2, "coed"}}
	if _, err = s.ReserveParty(ctx, "buyer", key, Full, Stripe, 3, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("changed party accepted", err)
	}
}

func TestPartyLastCabinsConcurrency(t *testing.T) {
	s, _ := testStore(t, 3)
	ctx := context.Background()
	selection := []CabinSelection{{"shared", 2, "coed"}, {"shared", 1, "female"}}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ReserveParty(ctx, "buyer", uuid.NewString(), Full, Stripe, 3, selection)
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
		t.Fatal("oversold party", success)
	}
}
