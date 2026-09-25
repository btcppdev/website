package servicebilling

import (
	"errors"
	"testing"
	"time"
)

func TestBitcoinClosureUsesActualPaidPeriod(t *testing.T) {
	start := time.Date(2024, 1, 31, 12, 0, 0, 0, time.UTC)
	for _, months := range []int{12, 24, 120} {
		price, err := AlertsTerm("opennode", 1, months, start)
		if err != nil {
			t.Fatal(err)
		}
		half := start.Add(price.End.Sub(start) / 2)
		for _, tc := range []struct {
			name        string
			at          time.Time
			floor, ceil int64
		}{
			{"before", start.Add(-time.Second), 101, 101},
			{"start", start, 101, 101},
			{"half", half, 50, 51},
			{"last instant", price.End.Add(-time.Nanosecond), 0, 1},
			{"end", price.End, 0, 0},
			{"after", price.End.Add(time.Hour), 0, 0},
		} {
			got, err := EstimateBitcoinClosureRefund(101, start, price.End, tc.at)
			if err != nil || got.FloorSatoshis != tc.floor || got.CeilingSatoshis != tc.ceil {
				t.Fatalf("%d months %s: %+v %v", months, tc.name, got, err)
			}
		}
	}
	// A leap-day service period has 29 actual days, not a 30-day denominator.
	end := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	got, err := EstimateBitcoinClosureRefund(2900, feb, end, feb.Add(28*24*time.Hour))
	if err != nil || got.FloorSatoshis != 100 || got.CeilingSatoshis != 100 {
		t.Fatal(got, err)
	}
	// Mid-term paid upgrades use their own start and amount.
	upgradeStart := feb.Add(19 * 24 * time.Hour)
	got, err = EstimateBitcoinClosureRefund(1000, upgradeStart, end, upgradeStart.Add(5*24*time.Hour))
	if err != nil || got.FloorSatoshis != 500 || got.CeilingSatoshis != 500 {
		t.Fatal("upgrade basis", got, err)
	}
}

func TestBitcoinClosureExactArithmeticAndValidation(t *testing.T) {
	start := time.Date(2500, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(10, 0, 0)
	const amount int64 = 2_100_000_000_000_000
	got, err := EstimateBitcoinClosureRefund(amount, start, end, start.Add(end.Sub(start)/2))
	if err != nil || got.FloorSatoshis != amount/2 || got.CeilingSatoshis != amount/2 {
		t.Fatal("overflow", got, err)
	}
	zone := time.FixedZone("offset", 3600)
	same, err := EstimateBitcoinClosureRefund(amount, start.In(zone), end.In(zone), got.ClosureAt.In(zone))
	if err != nil || same != got {
		t.Fatal("timezone changed value", same, got, err)
	}
	for _, tc := range []struct {
		amount         int64
		start, end, at time.Time
	}{
		{0, start, end, start}, {-1, start, end, start}, {amount + 1, start, end, start},
		{1, time.Time{}, end, start}, {1, start, time.Time{}, start}, {1, start, end, time.Time{}},
		{1, start, start, start}, {1, end, start, start},
	} {
		if _, err := EstimateBitcoinClosureRefund(tc.amount, tc.start, tc.end, tc.at); !errors.Is(err, ErrClosureRefund) {
			t.Fatal("invalid basis accepted", tc, err)
		}
	}
}
