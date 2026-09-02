package handlers

import (
	"context"
	"html/template"
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

func TestSiteAccountNavigationAnonymousDefaultsToDashboardLogin(t *testing.T) {
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	templates := template.Must(template.New("navigation").Parse(`{{ define "site_account_anonymous" }}<a href="/login">sign in</a>{{ end }}`))
	ctx := &config.AppContext{Session: manager, TemplateCache: templates}
	req := httptest.NewRequest(http.MethodGet, "/navigation/account?next=https://evil.example", nil).WithContext(requestContext)
	rec := httptest.NewRecorder()

	SiteAccountNavigation(rec, req, ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `href="/login"`) || strings.Contains(rec.Body.String(), `next=`) {
		t.Fatalf("anonymous navigation did not use the default dashboard login: %s", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestLogoutHandlerRequiresSessionCSRF(t *testing.T) {
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	manager.Put(requestContext, auth.SessionPersonIDKey, "person-id")
	manager.Put(requestContext, auth.SessionEmailKey, "new@example.test")
	manager.Put(requestContext, auth.SessionMethodKey, string(auth.MethodEmailLink))
	manager.Put(requestContext, authMethodsCSRFKey, "logout-csrf")
	ctx := &config.AppContext{Env: &types.EnvConfig{}, Session: manager}

	badForm := url.Values{"csrf": {"wrong"}}
	badReq := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(badForm.Encode())).WithContext(requestContext)
	badReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badRec := httptest.NewRecorder()
	LogoutHandler(badRec, badReq, ctx)
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF status = %d, want 403", badRec.Code)
	}
	if got := manager.GetString(requestContext, auth.SessionPersonIDKey); got != "person-id" {
		t.Fatalf("invalid CSRF cleared session person %q", got)
	}

	goodForm := url.Values{"csrf": {"logout-csrf"}}
	goodReq := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(goodForm.Encode())).WithContext(requestContext)
	goodReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	goodRec := httptest.NewRecorder()
	LogoutHandler(goodRec, goodReq, ctx)
	if goodRec.Code != http.StatusSeeOther || goodRec.Header().Get("Location") != "/" {
		t.Fatalf("valid logout = %d %q", goodRec.Code, goodRec.Header().Get("Location"))
	}
	if got := manager.GetString(requestContext, auth.SessionPersonIDKey); got != "" {
		t.Fatalf("valid logout retained session person %q", got)
	}
	if got := manager.GetString(requestContext, auth.SessionEmailKey); got != "" {
		t.Fatalf("valid logout retained pending account email %q", got)
	}
	if got := manager.GetString(requestContext, auth.SessionMethodKey); got != "" {
		t.Fatalf("valid logout retained pending authentication method %q", got)
	}
}
