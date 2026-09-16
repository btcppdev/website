package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestReviewActionForProposalDeclineInvitedMeansTheyDecline(t *testing.T) {
	action := reviewAction{Label: "Decline", Status: "WeDecline", Letter: "talkdeclined"}
	got := reviewActionForProposal(action, &types.Proposal{Status: "Invited"})

	if got.Status != "TheyDecline" {
		t.Fatalf("Status = %q, want TheyDecline", got.Status)
	}
	if got.Letter != "" {
		t.Fatalf("Letter = %q, want no letter", got.Letter)
	}
	if got.Label != "Speaker declined" {
		t.Fatalf("Label = %q, want Speaker declined", got.Label)
	}
}

func TestReviewActionForProposalDeclineAppliedStillMeansWeDecline(t *testing.T) {
	action := reviewAction{Label: "Decline", Status: "WeDecline", Letter: "talkdeclined"}
	got := reviewActionForProposal(action, &types.Proposal{Status: "Applied"})

	if got.Status != "WeDecline" {
		t.Fatalf("Status = %q, want WeDecline", got.Status)
	}
	if got.Letter != "talkdeclined" {
		t.Fatalf("Letter = %q, want talkdeclined", got.Letter)
	}
}

func TestNextProposalAfterDecision(t *testing.T) {
	for _, status := range []string{"Invited", "Accepted", "Waitlisted", "WeDecline", "Rejected"} {
		t.Run(status, func(t *testing.T) {
			proposals := []*types.Proposal{
				{ID: "first", Status: "Applied"},
				{ID: "current", Status: status},
				{ID: "already-decided", Status: "Accepted"},
				{ID: "next", Status: "InReview"},
			}
			if got := nextProposalAfter(proposals, "current"); got == nil || got.ID != "next" {
				t.Fatalf("next after decision = %+v, want next", got)
			}
			proposals[3].Status = status
			if got := nextProposalAfter(proposals, "next"); got == nil || got.ID != "first" {
				t.Fatalf("end should wrap to first pending: %+v", got)
			}
			proposals[0].Status = status
			if got := nextProposalAfter(proposals, "first"); got != nil {
				t.Fatalf("completed queue should be empty: %+v", got)
			}
		})
	}
}

func TestNextProposalAfterQueueEdges(t *testing.T) {
	for _, tc := range []struct {
		name       string
		proposals  []*types.Proposal
		from, want string
	}{
		{name: "empty"},
		{name: "only current", proposals: []*types.Proposal{{ID: "a", Status: "Applied"}}, from: "a"},
		{name: "missing anchor", proposals: []*types.Proposal{nil, {ID: "done", Status: "Rejected"}, {ID: "a", Status: "Applied"}}, from: "missing", want: "a"},
		{name: "skip wraps", proposals: []*types.Proposal{{ID: "a", Status: "Applied"}, {ID: "b", Status: "InReview"}}, from: "b", want: "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := nextProposalAfter(tc.proposals, tc.from)
			id := ""
			if got != nil {
				id = got.ID
			}
			if id != tc.want {
				t.Fatalf("next=%q, want %q", id, tc.want)
			}
		})
	}
}
