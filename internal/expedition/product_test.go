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
	if _, err := NewSchedule("shared", Installments, Stripe, time.Date(2027, 10, 1, 5, 0, 0, 0, time.UTC)); !errors.Is(err, ErrClosed) {
		t.Fatal("late installment signup accepted")
	}
}

func TestRemainingInstallments(t *testing.T) {
	for _, tc := range []struct {
		at    string
		count int
	}{
		{"2027-01-01T05:59:59Z", 5}, {"2027-01-01T06:00:00Z", 4},
		{"2027-04-01T04:59:59Z", 4}, {"2027-04-01T05:00:00Z", 3},
		{"2027-07-01T05:00:00Z", 2}, {"2027-10-01T04:59:59Z", 2},
		{"2027-10-01T05:00:00Z", 1},
	} {
		t.Run(tc.at, func(t *testing.T) {
			at, _ := time.Parse(time.RFC3339, tc.at)
			for _, fare := range Fares() {
				for _, provider := range []string{Stripe, Bitcoin} {
					s, err := NewSchedule(fare.ID, Installments, provider, at)
					if tc.count == 1 {
						if !errors.Is(err, ErrClosed) {
							t.Fatal("installments must close", err)
						}
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					if len(s.Installments) != tc.count {
						t.Fatalf("got %d payments", len(s.Installments))
					}
					var sum int64
					for i, p := range s.Installments {
						sum += p.Cents
						want := fare.Cents / int64(tc.count)
						if i == tc.count-1 {
							want += fare.Cents % int64(tc.count)
						}
						if p.Cents != want {
							t.Fatalf("payment %d: %d != %d", i, p.Cents, want)
						}
						deadline, _ := InstallmentDeadline(p.DueAt, "America/Chicago")
						if i > 0 && !deadline.After(at) {
							t.Fatal("past installment included")
						}
					}
					if sum != fare.Cents {
						t.Fatal("schedule does not sum to fare")
					}
				}
			}
		})
	}
}
