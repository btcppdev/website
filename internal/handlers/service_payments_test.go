package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
