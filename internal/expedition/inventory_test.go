package expedition

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStaffReserve(t *testing.T) {
	s, now := testStore(t, 18)
	ctx := context.Background()
	if err := s.SetStaffBerths(ctx, 3); err != nil {
		t.Fatal(err)
	}
	a, err := s.Availability(ctx)
	if err != nil || a.Total != 25 || a.Staff != 3 || a.Remaining != 22 {
		t.Fatal(a, err)
	}
	reserve(t, s, Full)
	if err = s.SetStaffBerths(ctx, 25); !errors.Is(err, ErrConflict) {
		t.Fatal("displaced live hold", err)
	}
	if err = s.SetStaffBerths(ctx, 24); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reserve(ctx, "other", "another-booking-key", "shared", Full, Stripe); !errors.Is(err, ErrSoldOut) {
		t.Fatal("sold a staff berth", err)
	}
	if err = s.SetStaffBerths(ctx, 23); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reserve(ctx, "other", "another-booking-key", "shared", Full, Stripe); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(16 * time.Minute)
	a, err = s.Availability(ctx)
	if err != nil || a.Remaining != 2 {
		t.Fatal("expired holds counted", a, err)
	}
}
