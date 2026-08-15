package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
)

func TestAuthRedirectInvalidLinkRedirectsToLoginWithError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/auth?em=not-base64&hr=also-bad&next=/dashboard/talks", nil)
	rec := httptest.NewRecorder()

	AuthRedirect(rec, req, &config.AppContext{})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/login?") {
		t.Fatalf("Location = %q, want /login redirect", location)
	}
	if !strings.Contains(location, "next=%2Fdashboard%2Ftalks") {
		t.Fatalf("Location = %q, missing preserved next", location)
	}
	if !strings.Contains(location, "error=") {
		t.Fatalf("Location = %q, missing error flash", location)
	}
}

func TestIdentityFromSpeakerUsesCanonicalPerson(t *testing.T) {
	speaker := &types.Speaker{ID: "person-id", Roles: []string{"toronto-admin", "toronto-staff"}}
	authenticatedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	id := identityFromSpeaker("person-id", MethodEmailLink, authenticatedAt, "alias@example.com", "primary@example.com", speaker)
	if id == nil || id.Speaker != speaker || id.PersonID != "person-id" {
		t.Fatalf("identity = %+v, want canonical person", id)
	}
	if id.LoginEmail != "alias@example.com" || id.PrimaryEmail != "primary@example.com" {
		t.Fatalf("identity emails = %q/%q", id.LoginEmail, id.PrimaryEmail)
	}
	if id.Method != MethodEmailLink || !id.AuthenticatedAt.Equal(authenticatedAt) {
		t.Fatalf("identity proof = %q at %s", id.Method, id.AuthenticatedAt)
	}
	if !id.HasRoleForConf("toronto", RoleAdmin) || len(id.Roles) != 2 {
		t.Fatalf("roles = %+v, want canonical person's roles", id.Roles)
	}
}

func TestLoginPersonStoresMethodRotatesTokenAndClearsEmailProof(t *testing.T) {
	manager := scs.New()
	initialContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	manager.Put(initialContext, SessionEmailKey, "stale@example.com")
	initialToken, _, err := manager.Commit(initialContext)
	if err != nil {
		t.Fatal(err)
	}

	loadedContext, err := manager.Load(context.Background(), initialToken)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil).WithContext(loadedContext)
	before := time.Now().UTC()
	if err := LoginPerson(&config.AppContext{Session: manager}, req, " person-id ", Method("passkey")); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()
	rotatedToken, _, err := manager.Commit(req.Context())
	if err != nil {
		t.Fatal(err)
	}

	if rotatedToken == initialToken {
		t.Fatal("login did not rotate the session token")
	}
	if got := manager.GetString(req.Context(), SessionPersonIDKey); got != "person-id" {
		t.Fatalf("person ID = %q", got)
	}
	if got := manager.GetString(req.Context(), SessionMethodKey); got != "passkey" {
		t.Fatalf("method = %q", got)
	}
	if got := manager.GetString(req.Context(), SessionEmailKey); got != "" {
		t.Fatalf("stale login email retained as %q", got)
	}
	authenticatedAt, err := time.Parse(time.RFC3339Nano, manager.GetString(req.Context(), SessionAuthenticatedAtKey))
	if err != nil {
		t.Fatalf("parse authentication time: %v", err)
	}
	if authenticatedAt.Before(before) || authenticatedAt.After(after) {
		t.Fatalf("authenticated at = %s, want between %s and %s", authenticatedAt, before, after)
	}
}

func TestLoginPersonRequiresMethod(t *testing.T) {
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil).WithContext(requestContext)
	if err := LoginPerson(&config.AppContext{Session: manager}, req, "person-id", ""); err == nil {
		t.Fatal("LoginPerson accepted an empty authentication method")
	}
}

func TestUpdateSessionEmailPreservesAuthenticationProof(t *testing.T) {
	manager := scs.New()
	initialContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	authenticatedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	manager.Put(initialContext, SessionPersonIDKey, "person-id")
	manager.Put(initialContext, SessionEmailKey, "old@example.com")
	manager.Put(initialContext, SessionMethodKey, "passkey")
	manager.Put(initialContext, SessionAuthenticatedAtKey, authenticatedAt)
	initialToken, _, err := manager.Commit(initialContext)
	if err != nil {
		t.Fatal(err)
	}

	loadedContext, err := manager.Load(context.Background(), initialToken)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/emails/primary", nil).WithContext(loadedContext)
	if err := UpdateSessionEmail(&config.AppContext{Session: manager}, req, "person-id", "NEW@example.com"); err != nil {
		t.Fatal(err)
	}
	rotatedToken, _, err := manager.Commit(req.Context())
	if err != nil {
		t.Fatal(err)
	}

	if rotatedToken == initialToken {
		t.Fatal("email update did not rotate the session token")
	}
	if got := manager.GetString(req.Context(), SessionEmailKey); got != "new@example.com" {
		t.Fatalf("email = %q", got)
	}
	if got := manager.GetString(req.Context(), SessionMethodKey); got != "passkey" {
		t.Fatalf("method changed to %q", got)
	}
	if got := manager.GetString(req.Context(), SessionAuthenticatedAtKey); got != authenticatedAt {
		t.Fatalf("authentication time changed to %q", got)
	}
}

func TestLogoutClearsIdentityAndProofMetadata(t *testing.T) {
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/logout", nil).WithContext(requestContext)
	manager.Put(req.Context(), SessionPersonIDKey, "person-id")
	manager.Put(req.Context(), SessionEmailKey, "person@example.com")
	manager.Put(req.Context(), SessionMethodKey, string(MethodEmailLink))
	manager.Put(req.Context(), SessionAuthenticatedAtKey, time.Now().UTC().Format(time.RFC3339Nano))

	Logout(&config.AppContext{Session: manager}, req)

	if got := manager.GetString(req.Context(), SessionPersonIDKey); got != "" {
		t.Fatalf("person ID retained as %q", got)
	}
	if got := manager.GetString(req.Context(), SessionEmailKey); got != "" {
		t.Fatalf("email retained as %q", got)
	}
	if got := manager.GetString(req.Context(), SessionMethodKey); got != "" {
		t.Fatalf("method retained as %q", got)
	}
	if got := manager.GetString(req.Context(), SessionAuthenticatedAtKey); got != "" {
		t.Fatalf("authentication time retained as %q", got)
	}
}

func TestHackathonRolesAreScopedAndCoveredByAdmin(t *testing.T) {
	manager := &Identity{Roles: ParseRoles([]string{"toronto-hackathon"})}
	if !manager.HasRoleForConf("toronto", RoleHackathon) {
		t.Fatal("conference hackathon role does not grant its conference")
	}
	if manager.HasRoleForConf("nairobi", RoleHackathon) {
		t.Fatal("conference hackathon role grants another conference")
	}

	globalManager := &Identity{Roles: ParseRoles([]string{"global-hackathon"})}
	if !globalManager.HasRoleForConf("toronto", RoleHackathon) || !globalManager.HasRoleForConf("nairobi", RoleHackathon) {
		t.Fatal("global hackathon role does not cover every conference")
	}

	admin := &Identity{Roles: ParseRoles([]string{"toronto-admin"})}
	if !admin.HasRoleForConf("toronto", RoleHackathon) {
		t.Fatal("conference admin does not cover hackathon management")
	}
	if admin.HasExactRoleForConf("toronto", RoleHackathon) {
		t.Fatal("admin was reported as an explicit hackathon manager")
	}

	volcoord := &Identity{Roles: ParseRoles([]string{"toronto-volcoord"})}
	if volcoord.HasRoleForConf("toronto", RoleHackathon) {
		t.Fatal("volunteer coordinator grants hackathon management")
	}
}
