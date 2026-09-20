package helpers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"net/url"
	"testing"
)

func TestSpeakerInviteLinkBindsRecipientProposalAndToken(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	link := SpeakerInviteLink(ctx, "panel", "token", "george")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("recipient") != "george" || q.Get("t") != "token" {
		t.Fatal("recipient not encoded")
	}
	for _, tc := range []struct {
		proposal, token, person string
		valid                   bool
	}{
		{"panel", "token", "george", true}, {"panel", "token", "misha", false}, {"other-panel", "token", "george", false}, {"panel", "rotated-token", "george", false},
	} {
		if VerifySpeakerInviteRecipient(ctx, tc.proposal, tc.token, tc.person, q.Get("signature")) != tc.valid {
			t.Fatalf("unexpected validation: %+v", tc)
		}
	}
}
