package types

import (
	"testing"
	"time"
)

func TestConfLocUsesKnownConferenceTimezoneFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		conf     *Conf
		wantZone string
	}{
		{
			name: "blank timezone",
			conf: &Conf{
				Tag:       "nairobi",
				StartDate: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC),
			},
			wantZone: "Africa/Nairobi",
		},
		{
			name: "utc timezone",
			conf: &Conf{
				Tag:       "nairobi",
				Timezone:  "UTC",
				TZ:        time.UTC,
				StartDate: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC),
			},
			wantZone: "Africa/Nairobi",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conf.Loc().String(); got != tc.wantZone {
				t.Fatalf("Loc() = %q, want %q", got, tc.wantZone)
			}
		})
	}
}
