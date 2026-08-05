package types

import "testing"

func TestCanonicalShirtSizes(t *testing.T) {
	wantCodes := []string{"LS", "LM", "LL", "MS", "MM", "ML", "MXL", "MXXL", "MXXXL"}
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
		if ShirtSizeLabel(want) == "" {
			t.Errorf("ShirtSizeLabel(%q) is empty", want)
		}
	}
	for _, legacy := range []string{"S", "M", "L", "XL", "XXL", "unknown"} {
		if got := ValidShirtSizeCode(legacy); got != "" {
			t.Errorf("ValidShirtSizeCode(%q) = %q, want empty", legacy, got)
		}
	}
}
