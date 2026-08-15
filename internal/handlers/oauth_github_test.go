package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/alexedwards/scs/v2"
)

func TestGitHubOAuthStartStoresOneTimePKCEFlow(t *testing.T) {
	ctx, req := githubOAuthTestContext(t, types.OAuthProviderConfig{ClientID: "client", ClientSecret: "secret"})
	req.URL, _ = url.Parse("/auth/oauth/github?next=/dashboard/talks")
	recorder := httptest.NewRecorder()

	GitHubOAuthStart(recorder, req, ctx)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Host != "github.com" || location.Path != "/login/oauth/authorize" {
		t.Fatalf("redirect = %s", location)
	}
	if got := ctx.Session.GetString(req.Context(), githubOAuthStateKey); got == "" || got != location.Query().Get("state") {
		t.Fatalf("stored state = %q; redirect state = %q", got, location.Query().Get("state"))
	}
	if ctx.Session.GetString(req.Context(), githubOAuthVerifierKey) == "" {
		t.Fatal("PKCE verifier was not stored")
	}
	if got := ctx.Session.GetString(req.Context(), githubOAuthNextKey); got != "/dashboard/talks" {
		t.Fatalf("next = %q", got)
	}
}

func TestPendingGitHubIdentityRoundTripAndExpiry(t *testing.T) {
	ctx, req := githubOAuthTestContext(t, types.OAuthProviderConfig{})
	identity := &types.PersonOAuthIdentity{
		Provider: "github", Subject: "42", Username: "octocat",
		Email: "octocat@example.com", EmailVerified: true, AvatarURL: "https://example.test/avatar",
	}
	if err := storePendingGitHubIdentity(ctx, req, identity, "/dashboard/emails"); err != nil {
		t.Fatal(err)
	}
	pending, err := pendingGitHubIdentity(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Identity.Subject != identity.Subject || pending.Identity.Email != identity.Email || !pending.Identity.EmailVerified || pending.CSRF == "" {
		t.Fatalf("pending identity = %+v", pending)
	}

	ctx.Session.Put(req.Context(), githubPendingStartedAtKey, time.Now().Add(-githubPendingIdentityTTL-time.Minute).UTC().Format(time.RFC3339Nano))
	if _, err := pendingGitHubIdentity(ctx, req); err == nil {
		t.Fatal("expired pending identity was accepted")
	}
}

func TestSecureTokenEqualRejectsEmptyAndDifferentTokens(t *testing.T) {
	if !secureTokenEqual("same-token", "same-token") {
		t.Fatal("matching token rejected")
	}
	for _, pair := range [][2]string{{"", ""}, {"token", ""}, {"token", "other"}, {"short", "longer"}} {
		if secureTokenEqual(pair[0], pair[1]) {
			t.Fatalf("accepted token pair %q/%q", pair[0], pair[1])
		}
	}
}

func githubOAuthTestContext(t *testing.T, oauthConfig types.OAuthProviderConfig) (*config.AppContext, *http.Request) {
	t.Helper()
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/github", nil).WithContext(requestContext)
	return &config.AppContext{
		Env:     &types.EnvConfig{Host: "localhost", Port: "8888", OAuth: types.OAuthConfig{GitHub: oauthConfig}},
		Session: manager, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0),
	}, req
}
