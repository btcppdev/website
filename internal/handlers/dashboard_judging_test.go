package handlers

import (
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
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

func TestAttachDashboardHackathonManagerRoles(t *testing.T) {
	activeConf := &types.Conf{
		Tag:           "toronto",
		ShowHackathon: true,
		StartDate:     time.Now().Add(24 * time.Hour),
		EndDate:       time.Now().Add(72 * time.Hour),
	}
	pastConf := &types.Conf{
		Tag:           "berlin",
		ShowHackathon: true,
		StartDate:     time.Now().Add(-12 * 24 * time.Hour),
		EndDate:       time.Now().Add(-10 * 24 * time.Hour),
	}
	activeBlock := &EventBlock{Conf: activeConf}
	id := &auth.Identity{Roles: []auth.Role{{Scope: auth.GlobalScope, Name: auth.RoleHackathon}}}

	active, past := attachDashboardHackathonManagerRoles([]*EventBlock{activeBlock}, nil, []*types.Conf{activeConf, pastConf}, id)

	if len(active) != 1 || active[0] != activeBlock || !activeBlock.IsHackathonManager() {
		t.Fatalf("active manager block = %+v", active)
	}
	if len(past) != 1 || past[0].Conf != pastConf || !past[0].IsHackathonManager() {
		t.Fatalf("past manager block = %+v", past)
	}
}

func TestAttachDashboardHackathonManagerRolesDoesNotLabelAdmin(t *testing.T) {
	conf := &types.Conf{Tag: "toronto", ShowHackathon: true}
	block := &EventBlock{Conf: conf}
	id := &auth.Identity{Roles: []auth.Role{{Scope: "toronto", Name: auth.RoleAdmin}}}

	active, _ := attachDashboardHackathonManagerRoles([]*EventBlock{block}, nil, []*types.Conf{conf}, id)

	if len(active) != 1 || active[0].IsHackathonManager() {
		t.Fatalf("admin was labeled as explicit hackathon manager: %+v", active)
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
