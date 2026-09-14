package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
	"testing"
	"time"
)

func TestSelectedScoreJudgeEventStaysAtEndAfterJudging(t *testing.T) {
	now := time.Now()
	firstStart := now.Add(-4 * time.Hour)
	firstEnd := now.Add(-3 * time.Hour)
	lastStart := now.Add(-2 * time.Hour)
	lastEnd := now.Add(-time.Hour)
	competition := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeAutomatic}
	events := []*types.JudgeEvent{
		{ID: "expo", StartsAt: &firstStart, EndsAt: &firstEnd},
		{ID: "finals", StartsAt: &lastStart, EndsAt: &lastEnd},
	}

	if got := selectedScoreJudgeEventID(competition, events, ""); got != "finals" {
		t.Fatalf("selectedScoreJudgeEventID() = %q, want final closed event", got)
	}
}

func TestSelectedScoreJudgeEventStartsAtFirstUpcomingEvent(t *testing.T) {
	now := time.Now()
	firstStart := now.Add(time.Hour)
	firstEnd := now.Add(2 * time.Hour)
	lastStart := now.Add(3 * time.Hour)
	lastEnd := now.Add(4 * time.Hour)
	competition := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeAutomatic}
	events := []*types.JudgeEvent{
		{ID: "expo", StartsAt: &firstStart, EndsAt: &firstEnd},
		{ID: "finals", StartsAt: &lastStart, EndsAt: &lastEnd},
	}

	if got := selectedScoreJudgeEventID(competition, events, ""); got != "expo" {
		t.Fatalf("selectedScoreJudgeEventID() = %q, want first upcoming event", got)
	}
}
