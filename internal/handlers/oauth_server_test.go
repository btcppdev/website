package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestOAuthRedirectURIValidationSupportsNativeAppsAndRestrictsHTTP(t *testing.T) {
	redirects, ok := parseOAuthRedirectURIs("https://app.example/callback\nmyapp://oauth/callback\nhttp://127.0.0.1:3456/callback")
	if !ok || len(redirects) != 3 {
		t.Fatalf("redirects = %#v, valid=%v", redirects, ok)
	}
	for _, invalid := range []string{
		"http://app.example/callback",
		"https://app.example/callback#fragment",
		"https://app.example/callback\nhttps://app.example/callback",
	} {
		if _, ok := parseOAuthRedirectURIs(invalid); ok {
			t.Fatalf("accepted invalid redirects %q", invalid)
		}
	}
}

func TestOAuthScopeValidationRequiresAllowlistedUniqueScopes(t *testing.T) {
	allowed := []string{"profile:self:read", "offline_access"}
	if scopes, ok := allowedOAuthScopes("profile:self:read offline_access", allowed); !ok || len(scopes) != 2 {
		t.Fatalf("scopes=%#v valid=%v", scopes, ok)
	}
	for _, invalid := range []string{"", "talks:write", "profile:self:read profile:self:read"} {
		if _, ok := allowedOAuthScopes(invalid, allowed); ok {
			t.Fatalf("accepted invalid scopes %q", invalid)
		}
	}
}

func TestOAuthServerMetadataAdvertisesOnlyImplementedSecureFlows(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	response := httptest.NewRecorder()
	OAuthServerMetadata(response, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil), ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	methods := document["code_challenge_methods_supported"].([]any)
	if len(methods) != 1 || methods[0] != "S256" {
		t.Fatalf("PKCE methods = %#v", methods)
	}
	grants := document["grant_types_supported"].([]any)
	if len(grants) != 2 {
		t.Fatalf("grants = %#v", grants)
	}
}

func TestOAuthBrowserCORSMatchesOnlyRegisteredRedirectOrigins(t *testing.T) {
	client := &types.OAuthClient{RedirectURIs: []string{"https://app.example/oauth/callback", "myapp://oauth/callback", "http://localhost:3000/callback"}}
	for _, allowed := range []string{"https://app.example", "http://localhost:3000"} {
		if !oauthClientAllowsOrigin(client, allowed) {
			t.Fatalf("origin %q was denied", allowed)
		}
	}
	for _, denied := range []string{"https://evil.example", "https://app.example.evil", "myapp://oauth"} {
		if oauthClientAllowsOrigin(client, denied) {
			t.Fatalf("origin %q was allowed", denied)
		}
	}
}
