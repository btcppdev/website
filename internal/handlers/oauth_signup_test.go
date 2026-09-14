package handlers

import (
	"net/url"
	"testing"

	"btcpp-web/internal/auth"
)

func TestOAuthSignupPreservesAuthorizationThroughProfileCreation(t *testing.T) {
	next := "/oauth/authorize?client_id=alerts&state=opaque&code_challenge=challenge&redirect_uri=http%3A%2F%2F127.0.0.1%3A8097%2Foauth%2Fcallback"
	for _, tc := range []struct {
		identity *auth.Identity
		path     string
	}{
		{nil, "/login"},
		{&auth.Identity{}, "/login"},
		{&auth.Identity{LoginEmail: "new@example.test"}, "/dashboard/profile"},
	} {
		dest, err := url.Parse(oauthAccountSetupURL(tc.identity, next))
		if err != nil || dest.Path != tc.path || dest.Query().Get("next") != next {
			t.Fatalf("destination %v, error %v", dest, err)
		}
	}
}

func TestOAuthProfileSetupUsesOnlyLocalAuthorizationRoute(t *testing.T) {
	for _, value := range []string{"https://example.test/oauth/authorize", "//example.test/oauth/authorize", "/oauth/authorize-other", "/dashboard", "%"} {
		if isOAuthProfileSetup(value) {
			t.Fatalf("accepted %q", value)
		}
	}
	if !isOAuthProfileSetup("/oauth/authorize?client_id=alerts&state=opaque") {
		t.Fatal("local OAuth setup rejected")
	}
}
