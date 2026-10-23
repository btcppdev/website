package expedition

import (
	"errors"
	"testing"
	"time"
)

var bookingTime = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func TestSchedules(t *testing.T) {
	for _, f := range Fares() {
		for _, provider := range []string{Stripe, Bitcoin} {
			for _, plan := range []string{Full, Installments} {
				s, err := NewSchedule(f.ID, plan, provider, bookingTime)
				if err != nil {
					t.Fatal(err)
				}
				want := f.Cents
				count := 5
				if plan == Full {
					count = 1
					if provider == Bitcoin {
						want = FullBitcoinCents(want)
					}
				}
				if s.TotalCents != want || len(s.Installments) != count {
					t.Fatalf("bad schedule: %+v", s)
				}
				var total int64
				for n, i := range s.Installments {
					total += i.Cents
					if i.Number != n+1 || i.Cents <= 0 {
						t.Fatalf("bad installment: %+v", i)
					}
					if n == 0 && !i.DueAt.Equal(bookingTime) {
						t.Fatal("deposit not due at booking")
					}
					if n > 0 && !i.DueAt.Equal(time.Date(2027, time.Month(1+(n-1)*3), 1, 0, 0, 0, 0, time.UTC)) {
						t.Fatal("wrong milestone")
					}
				}
				if total != want {
					t.Fatalf("schedule sum %d != %d", total, want)
				}
			}
		}
	}
	for _, v := range []struct{ berth, plan, provider string }{{"bogus", Full, Stripe}, {"shared", "held-bitcoin", Bitcoin}, {"shared", Full, "bitcoin"}} {
		if _, err := NewSchedule(v.berth, v.plan, v.provider, bookingTime); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid request accepted", v, err)
		}
	}
	if _, err := NewSchedule("shared", Installments, Stripe, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrClosed) {
		t.Fatal("late installment signup accepted")
	}
}
