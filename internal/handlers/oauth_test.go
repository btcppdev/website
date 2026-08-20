package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/mux"
)

func TestOAuthStartStoresOneTimePKCEFlow(t *testing.T) {
	ctx, req := oauthTestContext(t, types.OAuthConfig{GitHub: types.OAuthProviderConfig{ClientID: "client", ClientSecret: "secret"}})
	req.URL, _ = url.Parse("/auth/oauth/github?next=/dashboard/talks")
	req = mux.SetURLVars(req, map[string]string{"provider": "github"})
	recorder := httptest.NewRecorder()

	OAuthStart(recorder, req, ctx)

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
	if got := ctx.Session.GetString(req.Context(), oauthKey("github", "state")); got == "" || got != location.Query().Get("state") {
		t.Fatalf("stored state = %q; redirect state = %q", got, location.Query().Get("state"))
	}
	if ctx.Session.GetString(req.Context(), oauthKey("github", "verifier")) == "" {
		t.Fatal("PKCE verifier was not stored")
	}
	if got := ctx.Session.GetString(req.Context(), oauthKey("github", "next")); got != "/dashboard/talks" {
		t.Fatalf("next = %q", got)
	}
}

func TestMLHOAuthStartDoesNotRequirePKCE(t *testing.T) {
	ctx, req := oauthTestContext(t, types.OAuthConfig{MLH: types.OAuthProviderConfig{ClientID: "client", ClientSecret: "secret"}})
	req = mux.SetURLVars(req, map[string]string{"provider": "mlh"})
	recorder := httptest.NewRecorder()
	OAuthStart(recorder, req, ctx)
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d", recorder.Code)
	}
	location, _ := url.Parse(recorder.Header().Get("Location"))
	if location.Query().Get("code_challenge") != "" || ctx.Session.GetString(req.Context(), oauthKey("mlh", "verifier")) != "" {
		t.Fatalf("MLH unexpectedly used PKCE: %s", location)
	}
}

func TestPendingOAuthIdentityRoundTripAndExpiry(t *testing.T) {
	ctx, req := oauthTestContext(t, types.OAuthConfig{})
	identity := &types.PersonOAuthIdentity{Provider: "discord", Subject: "42", Username: "octocat", Email: "octocat@example.com", EmailVerified: true, AvatarURL: "https://example.test/avatar"}
	if err := storePendingOAuthIdentity(ctx, req, "discord", identity, "/dashboard/emails"); err != nil {
		t.Fatal(err)
	}
	pending, err := pendingOAuth(ctx, req, "discord")
	if err != nil {
		t.Fatal(err)
	}
	if pending.Identity.Subject != identity.Subject || pending.Identity.Email != identity.Email || !pending.Identity.EmailVerified || pending.CSRF == "" {
		t.Fatalf("pending identity = %+v", pending)
	}
	ctx.Session.Put(req.Context(), oauthPendingKey("discord", "started_at"), time.Now().Add(-oauthConfirmationTTL-time.Minute).UTC().Format(time.RFC3339Nano))
	if _, err := pendingOAuth(ctx, req, "discord"); err == nil {
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

func TestEnabledOAuthProviderViewsOnlyReturnsConfiguredProviders(t *testing.T) {
	env := &types.EnvConfig{OAuth: types.OAuthConfig{
		Discord: types.OAuthProviderConfig{ClientID: "discord", ClientSecret: "secret"},
		MLH:     types.OAuthProviderConfig{ClientID: "mlh", ClientSecret: "secret"},
	}}
	views := enabledOAuthProviderViews(env, "/dashboard/emails")
	if len(views) != 2 || views[0].Key != "discord" || views[1].Key != "mlh" {
		t.Fatalf("enabled provider views = %+v", views)
	}
	if views[0].LinkURL != "/auth/oauth/discord?next=%2Fdashboard%2Femails" {
		t.Fatalf("Discord link URL = %q", views[0].LinkURL)
	}
}

func TestAccountOAuthViewsDoNotOfferConnectedProviderAgain(t *testing.T) {
	env := &types.EnvConfig{OAuth: types.OAuthConfig{
		GitHub:  types.OAuthProviderConfig{ClientID: "github", ClientSecret: "secret"},
		Discord: types.OAuthProviderConfig{ClientID: "discord", ClientSecret: "secret"},
	}}
	identities := []*types.PersonOAuthIdentity{
		{ID: "github-identity", Provider: "github", Subject: "42", Username: "octocat"},
		// Defensive deduplication keeps the page at one row per provider even
		// if settings are rendered while an old database is being repaired.
		{ID: "duplicate-github", Provider: "github", Subject: "43", Username: "other"},
	}
	connected, available := accountOAuthViews(env, identities)
	if len(connected) != 1 || connected[0].Identity.ID != "github-identity" {
		t.Fatalf("connected OAuth views = %+v", connected)
	}
	for _, provider := range available {
		if provider.Key == "github" {
			t.Fatalf("connected GitHub provider was offered again: %+v", available)
		}
	}
	if len(available) != 3 {
		t.Fatalf("available OAuth providers = %+v, want Discord plus two unconfigured providers", available)
	}
}

func TestOAuthEmailConflictReason(t *testing.T) {
	viewerID := "person-a"
	for _, test := range []struct {
		name       string
		resolution *types.PersonEmailResolution
		want       string
	}{
		{name: "unused email", resolution: &types.PersonEmailResolution{}},
		{name: "same profile", resolution: &types.PersonEmailResolution{Alias: &types.PersonEmail{PersonID: viewerID}}},
		{name: "another profile", resolution: &types.PersonEmailResolution{Alias: &types.PersonEmail{PersonID: "person-b"}}, want: "verified_email_other_person"},
		{name: "legacy conflict", resolution: &types.PersonEmailResolution{ConflictPersonIDs: []string{"person-a", "person-b"}}, want: "ambiguous_verified_email"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := oauthEmailConflictReason(viewerID, test.resolution); got != test.want {
				t.Fatalf("conflict reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestOAuthEmailDisposition(t *testing.T) {
	verified := &types.PersonOAuthIdentity{Email: "person@example.com", EmailVerified: true}
	for _, test := range []struct {
		name       string
		identity   *types.PersonOAuthIdentity
		resolution *types.PersonEmailResolution
		want       oauthEmailDisposition
	}{
		{name: "unverified provider email falls back", identity: &types.PersonOAuthIdentity{Email: "person@example.com"}, resolution: &types.PersonEmailResolution{}, want: oauthEmailFallback},
		{name: "missing provider email falls back", identity: &types.PersonOAuthIdentity{EmailVerified: true}, resolution: &types.PersonEmailResolution{}, want: oauthEmailFallback},
		{name: "unused verified email creates profile", identity: verified, resolution: &types.PersonEmailResolution{}, want: oauthEmailCreateProfile},
		{name: "existing verified email requires magic link", identity: verified, resolution: &types.PersonEmailResolution{Alias: &types.PersonEmail{PersonID: "person-a"}}, want: oauthEmailRequireMagicLink},
		{name: "ambiguous email is rejected", identity: verified, resolution: &types.PersonEmailResolution{ConflictPersonIDs: []string{"person-a", "person-b"}}, want: oauthEmailAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := oauthEmailDispositionFor(test.identity, test.resolution); got != test.want {
				t.Fatalf("disposition = %d, want %d", got, test.want)
			}
		})
	}
}

func TestOAuthLinkSuccessDestination(t *testing.T) {
	for _, test := range []struct {
		name string
		next string
		want string
	}{
		{name: "first-time sign-in returns to dashboard", next: "/dashboard", want: "/dashboard"},
		{name: "settings link returns to settings", next: "/dashboard/settings", want: "/dashboard/settings"},
		{name: "missing destination falls back to settings", want: "/dashboard/settings"},
		{name: "external destination falls back to settings", next: "https://example.com", want: "/dashboard/settings"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := oauthLinkSuccessDestination(test.next); got != test.want {
				t.Fatalf("destination = %q, want %q", got, test.want)
			}
		})
	}
}

func TestOAuthLinkEmailMarkdownUsesProviderSpecificButton(t *testing.T) {
	markdown := oauthLinkEmailMarkdown("person@example.com", "GitLab", "https://btcpp.dev/auth/magic?token=test-token")
	for _, want := range []string{
		"We've received a request to link your GitLab account",
		"associated with **person@example.com**",
		"[Finish linking GitLab](button#https://btcpp.dev/auth/magic?token=test-token)",
		"expires in 72 hours and can only be used once",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("OAuth link email missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "sign in to your bitcoin++ account") {
		t.Fatalf("OAuth link email uses generic sign-in copy:\n%s", markdown)
	}
}

func oauthTestContext(t *testing.T, oauthConfig types.OAuthConfig) (*config.AppContext, *http.Request) {
	t.Helper()
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/github", nil).WithContext(requestContext)
	return &config.AppContext{
		Env:     &types.EnvConfig{Host: "localhost", Port: "8888", OAuth: oauthConfig},
		Session: manager, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0),
	}, req
}
