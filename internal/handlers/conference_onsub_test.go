package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestConferenceOnSubExpiryDefaultsToEventEnd(t *testing.T) {
	loc := time.FixedZone("conference", -5*60*60)
	end := time.Date(2026, time.August, 16, 17, 0, 0, 0, loc)
	got, err := conferenceOnSubExpiry("", &types.Conf{EndDate: end, TZ: loc})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Equal(end) {
		t.Fatalf("expiry = %v, want %s", got, end)
	}
}

func TestConferenceOnSubExpiryUsesEndOfSelectedDay(t *testing.T) {
	loc := time.FixedZone("conference", 2*60*60)
	got, err := conferenceOnSubExpiry("2026-09-03", &types.Conf{TZ: loc})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.September, 3, 23, 59, 59, 0, loc)
	if got == nil || !got.Equal(want) {
		t.Fatalf("expiry = %v, want %s", got, want)
	}
}
