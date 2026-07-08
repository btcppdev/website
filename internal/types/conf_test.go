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

func TestTalkAnchorTagUsesClipartBasename(t *testing.T) {
	for _, tc := range []struct {
		name    string
		clipart string
		want    string
	}{
		{
			name:    "plain filename",
			clipart: "package-relay.png",
			want:    "package-relay",
		},
		{
			name:    "local fixture path",
			clipart: "../static/img/floripa26/leading.png",
			want:    "leading",
		},
		{
			name:    "empty",
			clipart: "",
			want:    "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (&Talk{Clipart: tc.clipart}).AnchorTag(); got != tc.want {
				t.Fatalf("Talk.AnchorTag() = %q, want %q", got, tc.want)
			}
			if got := (&ConfTalk{Clipart: tc.clipart}).AnchorTag(); got != tc.want {
				t.Fatalf("ConfTalk.AnchorTag() = %q, want %q", got, tc.want)
			}
		})
	}
}
