package mtypes

import (
	"testing"
	"time"
)

func TestLetterCalcSendAtAcceptsRFC3339(t *testing.T) {
	letter := &Letter{Title: "Weekly", SendAt: "2026-08-11T10:00:00-05:00"}
	got, err := letter.CalcSendAt()
	if err != nil {
		t.Fatalf("CalcSendAt: %v", err)
	}
	want := time.Date(2026, time.August, 11, 15, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("CalcSendAt = %s, want %s", got, want)
	}
}
