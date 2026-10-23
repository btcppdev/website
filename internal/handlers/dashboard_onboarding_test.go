package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPendingEmailDashboardOnboardingPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires local postgres")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	manager := scs.New()
	ctx, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil).WithContext(ctx)
	app := &config.AppContext{DB: pool, Session: manager, Env: &types.EnvConfig{}, Infos: log.New(io.Discard, "", 0), Err: log.New(io.Discard, "", 0)}
	email := uuid.NewString() + "@example.test"
	if err := auth.LoginEmail(app, req, email); err != nil {
		t.Fatal(err)
	}
	defer enableAntarcticaSalesForTest(t, app)()
	app.Env.OpenNode.Key = "local-fixture"
	checkout := httptest.NewRequest(http.MethodGet, "/expeditions/antarctica/checkout", nil).WithContext(ctx)
	checkoutResponse := httptest.NewRecorder()
	AntarcticaCheckout(checkoutResponse, checkout, app)
	checkoutLocation, checkoutErr := url.Parse(checkoutResponse.Header().Get("Location"))
	if checkoutErr != nil || checkoutResponse.Code != http.StatusSeeOther || checkoutLocation.Path != "/dashboard/profile" || checkoutLocation.Query().Get("next") != "/expeditions/antarctica/checkout" {
		t.Fatalf("new email checkout did not reach setup: %d %s %v", checkoutResponse.Code, checkoutResponse.Header().Get("Location"), checkoutErr)
	}
	if manager.GetString(ctx, auth.SessionEmailKey) != email {
		t.Fatal("checkout lost verified email")
	}
	// A pending email may not create a booking by posting around profile setup.
	checkoutResponse = httptest.NewRecorder()
	AntarcticaCheckout(checkoutResponse, httptest.NewRequest(http.MethodPost, "/expeditions/antarctica/checkout", nil).WithContext(ctx), app)
	if checkoutResponse.Code != http.StatusUnauthorized {
		t.Fatal("pending email authorized purchase", checkoutResponse.Code)
	}
	w := httptest.NewRecorder()
	Dashboard(w, req, app)
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil || w.Code != http.StatusSeeOther || location.Path != "/dashboard/profile" || location.Query().Get("next") != "/dashboard" {
		t.Fatal("pending user not sent to setup", w.Code, w.Header(), err)
	}
	if manager.GetString(req.Context(), auth.SessionEmailKey) != email || manager.GetString(req.Context(), auth.SessionPersonIDKey) != "" {
		t.Fatal("pending email proof changed")
	}
	w = httptest.NewRecorder()
	DashboardPersonEmails(w, httptest.NewRequest(http.MethodGet, "/dashboard/settings", nil).WithContext(req.Context()), app)
	location, err = url.Parse(w.Header().Get("Location"))
	if err != nil || location.Path != "/dashboard/profile" || location.Query().Get("next") != "/dashboard/settings" {
		t.Fatal("pending settings visitor looped to login", w.Header(), err)
	}
	w = httptest.NewRecorder()
	if id := requirePersonIdentity(w, httptest.NewRequest(http.MethodPost, "/dashboard/settings", nil).WithContext(req.Context()), app); id != nil {
		t.Fatal("pending user authorized mutation")
	}
	location, err = url.Parse(w.Header().Get("Location"))
	if err != nil || location.Path != "/login" {
		t.Fatal("mutation guard changed", w.Header(), err)
	}
	// A visitor without email proof must still sign in, never enter setup.
	guest, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	Dashboard(w, httptest.NewRequest(http.MethodGet, "/dashboard", nil).WithContext(guest), app)
	location, err = url.Parse(w.Header().Get("Location"))
	if err != nil || w.Code != http.StatusSeeOther || location.Path != "/login" {
		t.Fatal("anonymous visitor bypassed login", w.Code, w.Header(), err)
	}
}
