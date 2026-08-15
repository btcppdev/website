package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/internal/types"

	"golang.org/x/oauth2"
)

func TestGitHubAuthorizationURLUsesStatePKCEAndMinimalScopes(t *testing.T) {
	provider := NewGitHubOAuthProvider(&types.EnvConfig{
		Host: "localhost", Port: "8888",
		OAuth: types.OAuthConfig{GitHub: types.OAuthProviderConfig{ClientID: "client", ClientSecret: "secret"}},
	})
	authorizationURL, err := provider.AuthorizationURL("state-value", "challenge-value")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("state") != "state-value" || query.Get("code_challenge") != "challenge-value" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("OAuth proof parameters = %v", query)
	}
	if query.Get("redirect_uri") != "http://localhost:8888/auth/oauth/github/callback" {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("scope") != "read:user user:email" {
		t.Fatalf("scope = %q", query.Get("scope"))
	}
	if strings.Contains(query.Get("scope"), "repo") {
		t.Fatalf("scope requests repository access: %q", query.Get("scope"))
	}
}

func TestGitHubExchangeAndFetchIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.FormValue("code") != "oauth-code" || r.FormValue("code_verifier") != "pkce-verifier" {
				t.Errorf("token form = %v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "github-token", "token_type": "bearer", "scope": "read:user,user:email"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer github-token" {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 123456789, "login": "octocat", "avatar_url": "https://avatars.example/octocat"})
		case "/user/emails":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"email": "unverified@example.com", "verified": false, "primary": false},
				{"email": "secondary@example.com", "verified": true, "primary": false},
				{"email": "PRIMARY@example.com", "verified": true, "primary": true},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := NewGitHubOAuthProvider(&types.EnvConfig{
		Host: "localhost", Port: "8888",
		OAuth: types.OAuthConfig{GitHub: types.OAuthProviderConfig{ClientID: "client", ClientSecret: "secret"}},
	})
	provider.config.Endpoint = oauth2.Endpoint{AuthURL: server.URL + "/authorize", TokenURL: server.URL + "/token"}
	provider.apiBaseURL = server.URL
	provider.httpClient = server.Client()

	token, err := provider.Exchange(t.Context(), "oauth-code", "pkce-verifier")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := provider.FetchIdentity(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Provider != OAuthProviderGitHub || identity.Subject != "123456789" || identity.Username != "octocat" {
		t.Fatalf("identity = %+v", identity)
	}
	if identity.Email != "primary@example.com" || !identity.EmailVerified {
		t.Fatalf("verified email = %q/%t", identity.Email, identity.EmailVerified)
	}
}

func TestGitHubProviderDisabledWithPartialOrMissingConfig(t *testing.T) {
	for _, config := range []types.OAuthProviderConfig{{}, {ClientID: "client"}, {ClientSecret: "secret"}} {
		provider := NewGitHubOAuthProvider(&types.EnvConfig{OAuth: types.OAuthConfig{GitHub: config}})
		if provider.Enabled() {
			t.Fatalf("provider enabled for config %+v", config)
		}
	}
}
