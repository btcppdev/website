package handlers

import (
	"errors"
	"testing"
	"time"
)

func TestCanvasPriceTracksDollarPeg(t *testing.T) {
	price := 100000.0
	calls := 0
	rate := &merchExchangeRate{fetch: func(string) (float64, error) { calls++; return price, nil }}
	if got := rate.sats(6500); got != "65k" {
		t.Fatalf("$65 at $100k/BTC: %s", got)
	}
	for _, tc := range []struct {
		cents uint
		want  string
	}{{6949, "69k"}, {6950, "70k"}, {7049, "70k"}} {
		if got := rate.sats(tc.cents); got != tc.want {
			t.Fatalf("rounding %d cents: got %s, want %s", tc.cents, got, tc.want)
		}
	}
	price = 130000
	if got := rate.sats(6500); got != "65k" || calls != 1 {
		t.Fatal("quote not cached", got, calls)
	}
	rate.until = time.Time{}
	if got := rate.sats(6500); got != "50k" {
		t.Fatal("quote did not refresh", got)
	}
	rate.until = time.Time{}
	rate.fetch = func(string) (float64, error) { calls++; return 0, errors.New("offline") }
	if got := rate.sats(6500); got != "" {
		t.Fatal("failed lookup used stale estimate", got)
	}
	count := calls
	rate.sats(6500)
	if calls != count {
		t.Fatal("failed lookup repeated")
	}
}
