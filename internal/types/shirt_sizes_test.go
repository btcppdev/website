package types

import "testing"

func TestCanonicalShirtSizes(t *testing.T) {
	wantCodes := []string{"MS", "MM", "ML", "MXL", "MXXL"}
	wantLabels := []string{"S", "M", "L", "XL", "XXL"}
	options := ShirtSizeOptions()
	if len(options) != len(wantCodes) {
		t.Fatalf("ShirtSizeOptions() returned %d options, want %d", len(options), len(wantCodes))
	}
	for i, want := range wantCodes {
		if options[i].Code != want {
			t.Errorf("option %d code = %q, want %q", i, options[i].Code, want)
		}
		if ValidShirtSizeCode(" "+want+" ") != want {
			t.Errorf("ValidShirtSizeCode(%q) did not normalize to %q", want, want)
		}
		if options[i].Label != wantLabels[i] || ShirtSizeLabel(want) != wantLabels[i] || options[i].Legacy {
			t.Errorf("option %d must show %q", i, wantLabels[i])
		}
	}
	for _, legacy := range []string{"S", "M", "L", "XL", "XXL", "unknown"} {
		if got := ValidShirtSizeCode(legacy); got != "" {
			t.Errorf("ValidShirtSizeCode(%q) = %q, want empty", legacy, got)
		}
	}
	for code, choice := range map[string]string{"LS": "MS", "LM": "MM", "LL": "ML", "MXXXL": "MXXXL"} {
		if ValidShirtSizeCode(code) != code || ShirtSizeChoiceCode(code) != choice {
			t.Fatalf("legacy selection %s was lost", code)
		}
	}
	legacy := ShirtSizeOptions("MXXXL")
	if len(legacy) != 6 || legacy[5].Code != "MXXXL" || !legacy[5].Legacy {
		t.Fatal("existing XXXL must survive an unrelated form save", legacy)
	}
}
