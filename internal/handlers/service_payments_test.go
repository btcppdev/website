package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"github.com/alexedwards/scs/v2"
)

func TestServicePaymentsRequiresAuthentication(t *testing.T) {
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{Session: manager, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	r := httptest.NewRequest(http.MethodGet, "/admin/service-payments?after=invalid", nil).WithContext(requestContext)
	w := httptest.NewRecorder()
	// No database: attempting to load report data before authorization would fail.
	AdminServicePayments(w, r, app)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login?") {
		t.Fatalf("unauthenticated response: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatal("missing private response headers")
	}
}

func TestServiceRefundReviewRequiresAuthentication(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		manager := scs.New()
		ctx, err := manager.Load(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		app := &config.AppContext{Session: manager, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
		r := httptest.NewRequest(method, "/admin/service-refunds", strings.NewReader("refund_reference=anything")).WithContext(ctx)
		w := httptest.NewRecorder()
		AdminServiceRefunds(w, r, app)
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login?") {
			t.Fatal("unauthorized refund review", w.Code)
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private data caching")
		}
	}
}

func TestServiceRefundReviewFormProtection(t *testing.T) {
	for _, role := range []string{"nairobi-admin", "global-speaker", ""} {
		w := httptest.NewRecorder()
		serveAdminServiceRefunds(w, httptest.NewRequest("POST", "/admin/service-refunds", nil), &config.AppContext{}, &auth.Identity{Roles: auth.ParseRoles([]string{role})})
		if w.Code != 403 {
			t.Fatal("role allowed", role, w.Code)
		}
	}
	for _, form := range []string{"", "csrf=wrong", "csrf=expected&csrf=expected"} {
		manager := scs.New()
		r := httptest.NewRequest("POST", "/admin/service-refunds", strings.NewReader(form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx, err := manager.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		manager.Put(ctx, authMethodsCSRFKey, "expected")
		w := httptest.NewRecorder()
		serveAdminServiceRefunds(w, r.WithContext(ctx), &config.AppContext{Session: manager}, &auth.Identity{Roles: auth.ParseRoles([]string{"global-admin"})})
		if w.Code != 403 {
			t.Fatal("bad token reached database", w.Code)
		}
	}
}
