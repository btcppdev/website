package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/alexedwards/scs/v2"
)

func TestDashboardDevLoginEnabledOnlyInDevelopment(t *testing.T) {
	if dashboardDevLoginEnabled(nil) {
		t.Fatal("nil context enabled development login")
	}
	if dashboardDevLoginEnabled(&config.AppContext{}) {
		t.Fatal("context without environment enabled development login")
	}
	if !dashboardDevLoginEnabled(&config.AppContext{Env: &types.EnvConfig{}}) {
		t.Fatal("development context did not enable development login")
	}
	if dashboardDevLoginEnabled(&config.AppContext{Env: &types.EnvConfig{}, InProduction: true}) {
		t.Fatal("production app context enabled development login")
	}
	if dashboardDevLoginEnabled(&config.AppContext{Env: &types.EnvConfig{Prod: true}}) {
		t.Fatal("production environment enabled development login")
	}
}

func TestDashboardIdentityEmailSupportsPersonBackedLogin(t *testing.T) {
	if got := dashboardIdentityEmail(&auth.Identity{PrimaryEmail: " primary@example.test ", LoginEmail: "login@example.test"}); got != "primary@example.test" {
		t.Fatalf("dashboard identity email = %q", got)
	}
	if got := dashboardIdentityEmail(&auth.Identity{LoginEmail: " alias@example.test "}); got != "alias@example.test" {
		t.Fatalf("dashboard fallback identity email = %q", got)
	}
	if got := dashboardIdentityEmail(nil); got != "" {
		t.Fatalf("nil dashboard identity email = %q", got)
	}
}

func TestLoginRejectsDevLoginInProduction(t *testing.T) {
	form := url.Values{"Email": {"rafael.silva@example.test"}, "Action": {"dev-login"}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	Login(w, r, &config.AppContext{Env: &types.EnvConfig{Prod: true}, InProduction: true})

	if w.Code != http.StatusNotFound {
		t.Fatalf("production dev login status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDashboardPostRedirectsToCanonicalLogin(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/dashboard", nil)
	w := httptest.NewRecorder()

	Dashboard(w, r, &config.AppContext{})

	if w.Code != http.StatusSeeOther {
		t.Fatalf("dashboard POST status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if location := w.Header().Get("Location"); location != "/login?next=%2Fdashboard" {
		t.Fatalf("dashboard POST location = %q", location)
	}
}

func TestDashboardRejectsEmailHMACWithoutSession(t *testing.T) {
	key, err := types.DeriveHMACKey("dashboard-session-only-test")
	if err != nil {
		t.Fatal(err)
	}
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &config.AppContext{Env: &types.EnvConfig{HMACKey: key}, Session: manager}
	req := httptest.NewRequest(http.MethodGet, "/dashboard?em=old-email&hr=old-hmac", nil).WithContext(requestContext)

	if gotEmail, _, err := validateVolEmail(req, ctx); err == nil || gotEmail != "" {
		t.Fatalf("dashboard bearer URL authenticated as %q with error %v", gotEmail, err)
	}
}

func TestSelfServicePagesAcceptDashboardSessionWithoutURLCredentials(t *testing.T) {
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/talk/dev26?from=dashboard", nil).WithContext(requestContext)
	manager.Put(req.Context(), auth.SessionEmailKey, "speaker@example.test")
	ctx := &config.AppContext{Env: &types.EnvConfig{}, Session: manager}

	if gotEmail, gotHMAC, err := validateVolEmail(req, ctx); err != nil || gotEmail != "speaker@example.test" || gotHMAC != "" {
		t.Fatalf("session self-service identity = %q/%q, %v", gotEmail, gotHMAC, err)
	}
}
