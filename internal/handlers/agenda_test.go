package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestAgendaSessionHeightTracksScheduledDuration(t *testing.T) {
	start := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(45 * time.Minute)
	session := &types.Session{Sched: &types.Times{Start: start, End: &end}}

	want := 45 * agendaPixelsPerMinute
	if got := agendaSessionHeight(session); got != want {
		t.Fatalf("agendaSessionHeight() = %.1f, want %.1f", got, want)
	}
}

func TestAgendaSessionHeightKeepsShortSessionsUsable(t *testing.T) {
	start := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(15 * time.Minute)
	session := &types.Session{Sched: &types.Times{Start: start, End: &end}}

	if got := agendaSessionHeight(session); got != agendaMinSessionHeight {
		t.Fatalf("agendaSessionHeight() = %.1f, want minimum %.1f", got, agendaMinSessionHeight)
	}
}
