package handlers

import (
	"strings"
	"testing"

	"btcpp-web/internal/auth"
)

func TestPreferredReauthenticationMethodUsesLastLogin(t *testing.T) {
	methods := []*ReauthenticationMethodView{
		{Key: "passkey"},
		{Key: "nostr"},
		{Key: "password"},
	}
	preferred, alternatives := preferredReauthenticationMethod(methods, auth.MethodPassword)
	if preferred == nil || preferred.Key != "password" {
		t.Fatalf("preferred method = %#v", preferred)
	}
	if len(alternatives) != 2 || alternatives[0].Key != "passkey" || alternatives[1].Key != "nostr" {
		t.Fatalf("alternatives = %#v", alternatives)
	}
}

func TestPreferredReauthenticationMethodFallsBackToEligibleMethod(t *testing.T) {
	methods := []*ReauthenticationMethodView{{Key: "passkey"}, {Key: "nostr"}}
	preferred, alternatives := preferredReauthenticationMethod(methods, auth.MethodEmailLink)
	if preferred == nil || preferred.Key != "passkey" || len(alternatives) != 1 || alternatives[0].Key != "nostr" {
		t.Fatalf("preferred = %#v, alternatives = %#v", preferred, alternatives)
	}
}

func TestReauthenticationURLKeepsOnlyLocalDestination(t *testing.T) {
	got := reauthenticationURL("/signer/authorize/resume?id=abc", true)
	if !strings.HasPrefix(got, "/reauth?") || !strings.Contains(got, "strength=strong") || !strings.Contains(got, "next=%2Fsigner%2Fauthorize%2Fresume%3Fid%3Dabc") {
		t.Fatalf("strong reauthentication URL = %q", got)
	}
	if got := reauthenticationURL("https://evil.example/steal", false); got != "/reauth?next=%2Fdashboard" {
		t.Fatalf("external destination survived: %q", got)
	}
}
