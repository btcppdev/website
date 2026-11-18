package api

import (
	"btcpp-web/internal/types"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInsiderIdentityRequiresExactScopeAndUsesCurrentRoles(t *testing.T) {
	token := &types.PersonAPIToken{ID: "token", PersonID: "reporter", Scopes: []string{"insider:read", "insider:write"}}
	person := &types.Speaker{ID: "reporter", Name: "Reporter", Roles: []string{"insider-reporter"}}
	router := protectedTestRouter(token, person, []*types.PersonEmail{{Email: "private@example.com"}})
	for _, tc := range []struct {
		scope, token string
		status       int
	}{
		{"insider:read", "", 401}, {"insider:read", "revoked", 401},
		{"insider:read", "test-secret", 200}, {"insider:write", "test-secret", 200},
		{"insider:schedule", "test-secret", 403}, {"insider:analytics", "test-secret", 403},
		{"identity:self:read", "test-secret", 400},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/me/insider?scope="+tc.scope, nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: got %d: %s", tc.scope, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private@example.com") {
			t.Fatal("identity response leaked email")
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("identity response is cacheable")
		}
	}
	person.Roles = []string{"reader"}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me/insider?scope=insider:read", nil)
	r.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	var result struct {
		Data struct {
			Roles, Scopes []string
			Scope         string
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data.Roles) != 1 || result.Data.Roles[0] != "reader" || result.Data.Scope != "insider:read" || len(result.Data.Scopes) != 2 {
		t.Fatalf("stale identity or missing scope: %s", w.Body.String())
	}
	token.Scopes = []string{"identity:self:read"}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("identity-only token elevated to Insider: %d", w.Code)
	}
}

func TestInsiderScopesAreSupported(t *testing.T) {
	for _, scope := range []string{"insider:read", "insider:write", "insider:review", "insider:schedule", "insider:analytics"} {
		if !types.ValidAPITokenScopes([]string{scope}) {
			t.Fatalf("scope unavailable: %s", scope)
		}
	}
}
