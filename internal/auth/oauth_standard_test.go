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

func TestStandardOAuthProviderAuthorizationURLs(t *testing.T) {
	env := &types.EnvConfig{Host: "localhost", Port: "8888", OAuth: types.OAuthConfig{
		Discord: configuredOAuth(), GitLab: configuredOAuth(), MLH: configuredOAuth(),
	}}
	mlh := NewMLHOAuthProvider(env)
	if mlh.config.Endpoint.TokenURL != "https://my.mlh.io/oauth/token" {
		t.Fatalf("MLH token endpoint = %q", mlh.config.Endpoint.TokenURL)
	}
	for _, test := range []struct {
		provider          OAuthProvider
		host, path, scope string
		pkce              bool
	}{
		{NewDiscordOAuthProvider(env), "discord.com", "/oauth2/authorize", "identify email", true},
		{NewGitLabOAuthProvider(env), "gitlab.com", "/oauth/authorize", "read_user", true},
		{mlh, "my.mlh.io", "/oauth/authorize", "public user:read:profile user:read:email", false},
	} {
		authorizationURL, err := test.provider.AuthorizationURL("state", "challenge")
		if err != nil {
			t.Fatalf("%s: %v", test.provider.Key(), err)
		}
		parsed, _ := url.Parse(authorizationURL)
		if parsed.Host != test.host || parsed.Path != test.path || parsed.Query().Get("state") != "state" || parsed.Query().Get("scope") != test.scope {
			t.Fatalf("%s authorization URL = %s", test.provider.Key(), authorizationURL)
		}
		if got := parsed.Query().Get("redirect_uri"); got != "http://localhost:8888/auth/oauth/"+test.provider.Key()+"/callback" {
			t.Fatalf("%s redirect = %q", test.provider.Key(), got)
		}
		if (parsed.Query().Get("code_challenge") != "") != test.pkce {
			t.Fatalf("%s PKCE query = %v", test.provider.Key(), parsed.Query())
		}
	}
}

func TestStandardOAuthProvidersExchangeAndFetchIdentity(t *testing.T) {
	tests := []struct {
		name, key, userJSON, subject, username, email string
		verified                                      bool
	}{
		{"discord", "discord", `{"id":"123","username":"sats","email":"SATS@example.com","verified":true,"avatar":"hash"}`, "123", "sats", "sats@example.com", true},
		{"gitlab", "gitlab", `{"id":456,"username":"miner","email":"MINER@example.com","confirmed_at":"2026-01-01T00:00:00Z","avatar_url":"https://example/avatar"}`, "456", "miner", "miner@example.com", true},
		{"mlh", "mlh", `{"id":"uuid-789","first_name":"Ada","last_name":"Lovelace","email":"ADA@example.com"}`, "uuid-789", "Ada Lovelace", "ada@example.com", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.FormValue("code") != "code" {
						t.Errorf("token form = %v", r.Form)
					}
					if test.key != "mlh" && r.FormValue("code_verifier") != "verifier" {
						t.Errorf("missing verifier: %v", r.Form)
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "token_type": "bearer"})
				case "/user":
					if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
						t.Errorf("authorization = %q", r.Header.Get("Authorization"))
					}
					_, _ = w.Write([]byte(test.userJSON))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			provider := testStandardProvider(test.key)
			provider.config.Endpoint = oauth2.Endpoint{AuthURL: server.URL + "/authorize", TokenURL: server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}
			provider.apiBaseURL = server.URL
			provider.userPath = "/user"
			provider.httpClient = server.Client()
			token, err := provider.Exchange(t.Context(), "code", "verifier")
			if err != nil {
				t.Fatal(err)
			}
			identity, err := provider.FetchIdentity(t.Context(), token)
			if err != nil {
				t.Fatal(err)
			}
			if identity.Provider != test.key || identity.Subject != test.subject || identity.Username != test.username || identity.Email != test.email || identity.EmailVerified != test.verified {
				t.Fatalf("identity = %+v", identity)
			}
		})
	}
}

func configuredOAuth() types.OAuthProviderConfig {
	return types.OAuthProviderConfig{ClientID: "client", ClientSecret: "secret"}
}

func TestStandardOAuthProviderReportsSafeIdentityErrorDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden","error_description":"profile scope was not granted"}`))
	}))
	defer server.Close()

	provider := testStandardProvider("mlh")
	provider.apiBaseURL = server.URL
	provider.userPath = "/user"
	provider.httpClient = server.Client()
	_, err := provider.FetchIdentity(t.Context(), &oauth2.Token{AccessToken: "secret", TokenType: "Bearer"})
	if err == nil || !strings.Contains(err.Error(), "forbidden: profile scope was not granted") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("identity error = %v", err)
	}
}

func testStandardProvider(key string) *standardOAuthProvider {
	env := &types.EnvConfig{Host: "localhost", Port: "8888", OAuth: types.OAuthConfig{Discord: configuredOAuth(), GitLab: configuredOAuth(), MLH: configuredOAuth()}}
	switch key {
	case "discord":
		return NewDiscordOAuthProvider(env)
	case "gitlab":
		return NewGitLabOAuthProvider(env)
	default:
		return NewMLHOAuthProvider(env)
	}
}
