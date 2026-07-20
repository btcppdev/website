package handlers

import "testing"

func TestMaxSessionDayHandlesDoubleDigitDays(t *testing.T) {
	// This is the lexicographic order that triggered /berlin24: day 10 sorts
	// before day 9, so using the final key allocated only nine day buckets.
	keys := []string{"1+", "10+", "10=", "2+", "9-"}
	got, err := maxSessionDay(keys)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10 {
		t.Fatalf("maxSessionDay() = %d, want 10", got)
	}
}

func TestMaxSessionDayRejectsInvalidKey(t *testing.T) {
	if _, err := maxSessionDay([]string{"1+", "invalid"}); err == nil {
		t.Fatal("maxSessionDay() accepted an invalid session key")
	}
}
