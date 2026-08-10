package handlers

import (
	"testing"
	"time"
)

func TestNextWeeklyNewsletterDraftAtUsesMondayTwoCentral(t *testing.T) {
	chicago := weeklyNewsletterCentralLocation()
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "monday before two",
			now:  time.Date(2026, time.August, 10, 13, 59, 0, 0, chicago),
			want: time.Date(2026, time.August, 10, 14, 0, 0, 0, chicago),
		},
		{
			name: "monday at two moves to next week",
			now:  time.Date(2026, time.August, 10, 14, 0, 0, 0, chicago),
			want: time.Date(2026, time.August, 17, 14, 0, 0, 0, chicago),
		},
		{
			name: "sunday crosses daylight saving boundary",
			now:  time.Date(2026, time.October, 31, 14, 0, 0, 0, chicago),
			want: time.Date(2026, time.November, 2, 14, 0, 0, 0, chicago),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextWeeklyNewsletterDraftAt(tt.now); !got.Equal(tt.want) || got.Hour() != 14 || got.Location().String() != "America/Chicago" {
				t.Fatalf("nextWeeklyNewsletterDraftAt(%s) = %s, want %s", tt.now, got, tt.want)
			}
		})
	}
}

func TestWeeklyNewsletterDraftIsDueOnlyMondayAfterTwoCentral(t *testing.T) {
	chicago := weeklyNewsletterCentralLocation()
	for _, tt := range []struct {
		at   time.Time
		want bool
	}{
		{time.Date(2026, time.August, 10, 13, 59, 0, 0, chicago), false},
		{time.Date(2026, time.August, 10, 14, 0, 0, 0, chicago), true},
		{time.Date(2026, time.August, 10, 23, 59, 0, 0, chicago), true},
		{time.Date(2026, time.August, 11, 14, 0, 0, 0, chicago), false},
	} {
		if got := weeklyNewsletterDraftIsDue(tt.at); got != tt.want {
			t.Errorf("weeklyNewsletterDraftIsDue(%s) = %t, want %t", tt.at, got, tt.want)
		}
	}
}
