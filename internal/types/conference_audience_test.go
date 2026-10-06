package types

import "testing"

func TestConferenceAudienceList(t *testing.T) {
	for _, tc := range []struct{ tag, audience, want string }{
		{"seoul", "speakers", "seoul-speaker"},
		{"seoul", "attendees", "seoul-genpop"},
		{"seoul", "volunteers", "seoul-volunteer"},
		{"berlin26", "speakers", "berlin26-speaker"},
		{"", "speakers", ""},
		{"seoul", "unknown", ""},
	} {
		if got := ConferenceAudienceList(tc.tag, tc.audience); got != tc.want {
			t.Errorf("audience(%q, %q) = %q, want %q", tc.tag, tc.audience, got, tc.want)
		}
	}
}
