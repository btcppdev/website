package expedition

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCabinGroups(t *testing.T) {
	s, now := testStore(t, 18)
	ctx := context.Background()
	book := func(mode string, n int) (Reservation, error) {
		return s.ReserveGroup(ctx, "buyer", uuid.NewString(), "shared", Full, Bitcoin, mode, n)
	}
	a, err := book("male", 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := book("female", 1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := book("male", 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.CabinID == b.CabinID || a.CabinID != c.CabinID {
		t.Fatal("incompatible cabin assignment", a, b, c)
	}
	g, err := book("coed", 2)
	if err != nil {
		t.Fatal(err)
	}
	if g.CabinID == a.CabinID || g.CabinID == b.CabinID || g.Quantity != 2 || g.Schedule.TotalCents != 3121510 {
		t.Fatal("group split or mispriced", g)
	}
	if _, err = book("coed", 3); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized twin group accepted", err)
	}
	if _, err = book("any", 1); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid sharing accepted", err)
	}
	key := uuid.NewString()
	r, err := s.ReserveGroup(ctx, "buyer", key, "shared", Installments, Stripe, "coed", 1)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ReserveGroup(ctx, "buyer", key, "shared", Installments, Stripe, "coed", 1)
	if err != nil || replay.ID != r.ID {
		t.Fatal("group retry", err)
	}
	if _, err = s.ReserveGroup(ctx, "buyer", key, "shared", Installments, Stripe, "female", 1); !errors.Is(err, ErrConflict) {
		t.Fatal("sharing retry conflict", err)
	}
	if _, err = s.ReserveGroup(ctx, "buyer", key, "shared", Installments, Stripe, "coed", 2); !errors.Is(err, ErrConflict) {
		t.Fatal("quantity retry conflict", err)
	}
	*now = now.Add(16 * time.Minute)
	fresh, err := book("female", 2)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.CabinID != a.CabinID {
		t.Fatal("expired cabin preference did not reset", fresh)
	}
}

func TestCabinFragmentation(t *testing.T) {
	s, _ := testStore(t, 18)
	ctx := context.Background()
	// Leave one berth in each of two cabins; a pair must not be split across them.
	if _, err := s.DB.Exec(ctx, `UPDATE expedition_cabins SET enabled=false WHERE id NOT IN ('twin-1','twin-2')`); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"male", "female"} {
		if _, err := s.ReserveGroup(ctx, "buyer", uuid.NewString(), "shared", Full, Stripe, mode, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReserveGroup(ctx, "buyer", uuid.NewString(), "shared", Full, Stripe, "coed", 2); !errors.Is(err, ErrSoldOut) {
		t.Fatal("fragmented group accepted", err)
	}
}

func TestGroupSchedule(t *testing.T) {
	for _, f := range Fares() {
		for _, plan := range []string{Full, Installments} {
			for _, provider := range []string{Stripe, Bitcoin} {
				one, err := NewSchedule(f.ID, plan, provider, bookingTime)
				if err != nil {
					t.Fatal(err)
				}
				group, err := NewGroupSchedule(f.ID, plan, provider, 2, bookingTime)
				if err != nil {
					t.Fatal(err)
				}
				if group.TotalCents != 2*one.TotalCents {
					t.Fatal("group total")
				}
				for n, i := range group.Installments {
					if i.Cents != 2*one.Installments[n].Cents {
						t.Fatal("group installment")
					}
				}
			}
		}
	}
}

func TestCabinPreferenceIsRequest(t *testing.T) {
	s, _ := testStore(t, 2)
	ctx := context.Background()
	if _, err := s.DB.Exec(ctx, `UPDATE expedition_cabins SET enabled=false WHERE id!='twin-1'`); err != nil {
		t.Fatal(err)
	}
	a, err := s.ReserveGroup(ctx, "buyer", uuid.NewString(), "shared", Full, Stripe, "male", 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.ReserveGroup(ctx, "buyer", uuid.NewString(), "shared", Full, Stripe, "female", 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.CabinID != b.CabinID || b.Sharing != "female" {
		t.Fatal("preference became a hard restriction or was lost")
	}
}
