package handlers

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

func TestInvitationIdentityCannotSwitchPanelists(t *testing.T) {
	george := &types.Speaker{ID: "george", Email: "george@lightning.engineering"}
	misha := &types.Speaker{ID: "misha", Email: "misha@example.test"}
	invitee := &types.SpeakerConf{Speaker: george}
	for _, tc := range []struct {
		name    string
		invitee *types.SpeakerConf
		person  *types.Speaker
		email   string
		allowed bool
	}{
		{"intended recipient", invitee, george, george.Email, true},
		{"changed email", invitee, misha, misha.Email, false},
		{"different owner", invitee, misha, george.Email, false},
		{"missing account", invitee, nil, george.Email, false},
		{"legacy link existing account", nil, misha, misha.Email, false},
		{"legacy link new account", nil, nil, "new@example.test", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSpeakerInviteIdentity(tc.invitee, tc.person, tc.email); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}

func TestSharedPanelInvitationNeverGuessesRecipient(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	proposal := &types.Proposal{ID: "panel", InviteToken: "shared-token", SpeakerConfRefs: []string{"misha", "second", "third", "george"}}
	// No database is needed: a legacy shared link must not inspect panel members.
	sc, err := resolveSpeakerInviteRecipient(ctx, httptest.NewRequest("GET", "/invite-speaker/panel?t=shared-token", nil), proposal)
	if err != nil || sc != nil {
		t.Fatalf("recipient=%+v err=%v", sc, err)
	}
	link := helpers.SpeakerInviteLink(ctx, proposal.ID, proposal.InviteToken, "george")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("recipient", "misha")
	u.RawQuery = q.Encode()
	if _, err := resolveSpeakerInviteRecipient(ctx, httptest.NewRequest("GET", u.String(), nil), proposal); err == nil {
		t.Fatal("recipient substitution accepted")
	}
}

func TestPanelInvitationSelectsSignedPersonRegardlessOfViewedOrder(t *testing.T) {
	now := time.Now()
	speakers := map[string]*types.SpeakerConf{
		"first":  {Speaker: &types.Speaker{ID: "misha"}, InvitedAt: &now},
		"second": {Speaker: &types.Speaker{ID: "liam"}},
		"third":  {Speaker: &types.Speaker{ID: "steven"}},
		"fourth": {Speaker: &types.Speaker{ID: "george"}, InvitedAt: &now, ViewedAt: &now},
	}
	load := func(ref string) (*types.SpeakerConf, error) { return speakers[ref], nil }
	got, err := findSpeakerInviteRecipient([]string{"first", "second", "third", "fourth"}, "george", load)
	if err != nil || got != speakers["fourth"] {
		t.Fatalf("recipient=%+v err=%v", got, err)
	}
	if _, err := findSpeakerInviteRecipient([]string{"first", "second", "third"}, "george", load); err == nil {
		t.Fatal("accepted recipient absent from panel")
	}
}
