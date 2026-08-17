package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
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

func TestDashboardRejectsDevLoginInProduction(t *testing.T) {
	form := url.Values{"Email": {"rafael.silva@example.test"}, "Action": {"dev-login"}}
	r := httptest.NewRequest(http.MethodPost, "/dashboard", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	Dashboard(w, r, &config.AppContext{Env: &types.EnvConfig{Prod: true}, InProduction: true})

	if w.Code != http.StatusNotFound {
		t.Fatalf("production dev login status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
