package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestDashboardPendingSpeakerInvitations(t *testing.T) {
	invited := &types.Proposal{ID: "invited", Status: "Invited"}
	accepted := &types.Proposal{ID: "accepted", Status: "Accepted"}
	blocks := []*EventBlock{
		{SpeakerConf: &types.SpeakerConf{Proposals: []*types.Proposal{invited, accepted}}},
		{SpeakerConf: &types.SpeakerConf{Proposals: []*types.Proposal{invited}}},
	}
	got := dashboardPendingSpeakerInvitations(blocks)
	if len(got) != 1 || got[0].ID != invited.ID {
		t.Fatalf("pending invitations = %+v, want invited proposal once", got)
	}
}
