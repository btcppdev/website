package servicebilling

import (
	"math"
	"testing"
)

func TestBitcoinRefundUSDAllocation(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		original, total, priorSats, priorCents, refund, want int64
	}{
		{"floor", 1001, 1000, 0, 0, 500, 499},
		{"prior credit", 1001, 1000, 200, 199, 300, 300},
		{"sub-cent", 1000000, 100, 0, 0, 1, 0},
		{"cross cent", 1000000, 100, 9999, 0, 1, 1},
		{"final penny", 1001, 1000, 1000, 999, 1, 1},
		{"full", 1001, 1000, 0, 0, 1001, 1000},
		{"nothing left", 1001, 1000, 1001, 1000, 0, 0},
		{"overflow-safe", 2100000000000000, math.MaxInt64, 0, 0, 2100000000000000, math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AllocateBitcoinRefundUSD(tc.original, tc.total, tc.priorSats, tc.priorCents, tc.refund)
			if err != nil || got.AmountCents != tc.want || got.CumulativeRefundedCents != tc.priorCents+tc.want {
				t.Fatalf("allocation: %+v %v", got, err)
			}
		})
	}
}

func TestBitcoinRefundUSDAllocationInvalid(t *testing.T) {
	for _, args := range [][5]int64{{0, 100, 0, 0, 1}, {100, 0, 0, 0, 1}, {100, 100, -1, 0, 1}, {100, 100, 0, -1, 1}, {100, 100, 99, 99, 2}, {100, 100, 101, 100, 0}, {100, 100, 0, 0, -1}, {1001, 1000, 200, 100, 300}, {1001, 1000, 200, 200, 300}, {100, 100, 0, 101, 1}, {2100000000000001, 100, 0, 0, 1}} {
		if got, err := AllocateBitcoinRefundUSD(args[0], args[1], args[2], args[3], args[4]); err == nil || got != nil {
			t.Fatalf("invalid basis accepted: %v %+v", args, got)
		}
	}
}

func TestBitcoinRefundUSDAllocationDoesNotDependOnBatching(t *testing.T) {
	for _, total := range []int64{1, 17, 1000, 100000} {
		var sats, cents int64
		for _, part := range []int64{1, 2, 7, 31, 311, 647, 2} {
			allocation, err := AllocateBitcoinRefundUSD(1001, total, sats, cents, part)
			if err != nil {
				t.Fatal(err)
			}
			sats += part
			cents += allocation.AmountCents
		}
		if sats != 1001 || cents != total {
			t.Fatalf("split refund lost cents: sats=%d cents=%d want=%d", sats, cents, total)
		}
	}
}
