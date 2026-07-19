package handlers

import (
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
)

func TestAttachJudgeEventBlocksCreatesJudgeOnlyConferenceCard(t *testing.T) {
	conf := &types.Conf{
		Ref:       "conf-id",
		Tag:       "toronto",
		Active:    true,
		StartDate: time.Now().Add(24 * time.Hour),
		EndDate:   time.Now().Add(72 * time.Hour),
	}
	assignments := []*types.CompetitionJudgeAssignment{
		{ConferenceID: conf.Ref, ConferenceTag: conf.Tag, JudgeType: getters.JudgeTypeFinals},
	}

	active, past := attachJudgeEventBlocks(nil, nil, assignments, []*types.Conf{conf})

	if len(past) != 0 || len(active) != 1 {
		t.Fatalf("active=%d past=%d, want active=1 past=0", len(active), len(past))
	}
	block := active[0]
	if block.Conf != conf || !block.IsHackathonJudge() {
		t.Fatalf("judge-only event block = %+v", block)
	}
	if got := block.HackathonJudgeLabel(); got != "Hackathon judge" {
		t.Fatalf("HackathonJudgeLabel() = %q, want Hackathon judge", got)
	}
}

func TestAttachJudgeEventBlocksReusesExistingCardAndDeduplicatesTypes(t *testing.T) {
	conf := &types.Conf{
		Ref:       "conf-id",
		Tag:       "toronto",
		StartDate: time.Now().Add(24 * time.Hour),
		EndDate:   time.Now().Add(72 * time.Hour),
	}
	block := &EventBlock{Conf: conf}
	assignments := []*types.CompetitionJudgeAssignment{
		{ConferenceTag: conf.Tag, JudgeType: getters.JudgeTypeExpo},
		{ConferenceTag: conf.Tag, JudgeType: getters.JudgeTypeExpo},
		{ConferenceTag: conf.Tag, JudgeType: getters.JudgeTypeFinals},
	}

	active, _ := attachJudgeEventBlocks([]*EventBlock{block}, nil, assignments, []*types.Conf{conf})

	if len(active) != 1 || active[0] != block {
		t.Fatalf("attach created a duplicate conference card: %+v", active)
	}
	if len(block.JudgeTypes) != 2 {
		t.Fatalf("JudgeTypes = %v, want two unique roles", block.JudgeTypes)
	}
	if got := block.HackathonJudgeLabel(); got != "Hackathon judge" {
		t.Fatalf("HackathonJudgeLabel() = %q, want Hackathon judge", got)
	}
}
