package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestClockWidgetData(t *testing.T) {
	for _, tc := range []struct {
		name, timezone, instant, wantZone, wantTime string
	}{
		{"summer", "Europe/Berlin", "2026-09-11T12:34:56Z", "Europe/Berlin", "14:34:56"},
		{"winter", "Europe/Berlin", "2026-12-11T12:34:56Z", "Europe/Berlin", "13:34:56"},
		{"midnight", "Europe/Berlin", "2026-09-11T22:00:00Z", "Europe/Berlin", "00:00:00"},
		{"invalid", "invalid/timezone", "2026-09-11T12:34:56Z", "UTC", "12:34:56"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.instant)
			if err != nil {
				t.Fatal(err)
			}
			nav := NavConfList{Upcoming: []*types.Conf{{Timezone: tc.timezone}, {Timezone: "Asia/Tokyo"}}}
			got := clockWidgetData(nav, now)
			if got.Timezone != tc.wantZone || got.Time != tc.wantTime {
				t.Fatalf("unexpected clock: %+v", got)
			}
		})
	}
	if got := clockWidgetData(NavConfList{}, time.Date(2026, 9, 11, 1, 2, 3, 0, time.UTC)); got.Timezone != "UTC" || got.Time != "01:02:03" {
		t.Fatalf("unexpected fallback: %+v", got)
	}
}
