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
