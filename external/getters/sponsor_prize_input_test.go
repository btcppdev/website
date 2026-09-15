package getters

import "testing"

func TestSponsorPrizeInput(t *testing.T) {
	for _, tc := range []struct {
		name, kind, title, value string
		valid                    bool
	}{
		{"bitcoin automatic title", PrizeTypeSats, "", "1000000", true},
		{"physical no estimate", PrizeTypeInKind, "Hardware wallet", "", true},
		{"physical estimate", PrizeTypeInKind, "Hardware wallet", "50000", true},
		{"bitcoin missing amount", PrizeTypeSats, "", "", false},
		{"bitcoin zero", PrizeTypeSats, "", "0", false},
		{"fractional amount", PrizeTypeSats, "", "1.5", false},
		{"physical missing description", PrizeTypeInKind, "", "", false},
		{"invalid estimate", PrizeTypeInKind, "Wallet", "-1", false},
		{"tickets forbidden", PrizeTypeTickets, "Two tickets", "100000", false},
		{"trophy must use physical", PrizeTypeTrophy, "Trophy", "10000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeSponsorAwardProposalInput(SponsorAwardProposalInput{Title: "Best privacy project", MaxAwardees: 2, PrizeType: tc.kind, PrizeTitle: tc.title, PrizeValueText: tc.value})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if err == nil && tc.kind == PrizeTypeSats && got.PrizeTitle != "1000000 sats per winner" {
				t.Fatalf("unexpected automatic title: %q", got.PrizeTitle)
			}
		})
	}
}

func TestPhysicalPrizeCanOmitEstimate(t *testing.T) {
	for _, kind := range []string{PrizeTypeInKind, PrizeTypeSats, PrizeTypeTickets} {
		_, err := validatePrizeInput(PrizeInput{AwardID: "award", Title: "Prize", PrizeType: kind})
		if (err == nil) != (kind == PrizeTypeInKind) {
			t.Fatalf("kind %s: %v", kind, err)
		}
	}
}
