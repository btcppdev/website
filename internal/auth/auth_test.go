package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
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
	id := identityFromSpeaker("person-id", "alias@example.com", "primary@example.com", speaker)
	if id == nil || id.Speaker != speaker || id.PersonID != "person-id" {
		t.Fatalf("identity = %+v, want canonical person", id)
	}
	if id.LoginEmail != "alias@example.com" || id.PrimaryEmail != "primary@example.com" {
		t.Fatalf("identity emails = %q/%q", id.LoginEmail, id.PrimaryEmail)
	}
	if !id.HasRoleForConf("toronto", RoleAdmin) || len(id.Roles) != 2 {
		t.Fatalf("roles = %+v, want canonical person's roles", id.Roles)
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
