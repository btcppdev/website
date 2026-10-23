package handlers

import (
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"context"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"html/template"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestInsiderHiddenByDefault(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if insiderVisible(app) {
		t.Fatal("missing configuration enabled Insider")
	}
	w := httptest.NewRecorder()
	DashboardInsiderPreferences(w, httptest.NewRequest("GET", "/dashboard/insider", nil), app)
	if w.Code != 404 {
		t.Fatalf("disabled route: %d", w.Code)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		tm, err := app.TemplateCache.Clone()
		if err != nil {
			t.Fatal(err)
		}
		tm.Funcs(template.FuncMap{"insiderVisible": func() bool { return enabled }})
		for _, name := range []string{"dashboard_tabs", "sponsor_marquee_brands"} {
			var out bytes.Buffer
			if err := tm.ExecuteTemplate(&out, name, map[string]any{"ShowAdmin": true}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(out.String()), "insider") != enabled {
				t.Fatalf("%s visibility=%v", name, enabled)
			}
		}
		var out bytes.Buffer
		if err := tm.ExecuteTemplate(&out, "dashboard_subscriptions.tmpl", accountSubscriptionsPage{Subscriptions: []accountSubscription{{Name: "Alerts"}}}); err != nil {
			t.Fatal(err)
		}
		if !enabled && strings.Contains(strings.ToLower(out.String()), "insider") {
			t.Fatal("disabled subscriptions page mentions Insider")
		}
	}
}

func TestSiteFeatureAdminPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	admin, ordinary := uuid.NewString(), uuid.NewString()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO people(id,name) VALUES($1,'Feature admin'),($2,'Ordinary account')`, admin, ordinary)
	exec(`INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, admin)
	var initial bool
	if err := pool.QueryRow(context.Background(), `SELECT enabled FROM site_features WHERE name='insider'`).Scan(&initial); err != nil {
		t.Fatal(err)
	}
	defer func() {
		exec(`UPDATE site_features SET enabled=$1,updated_by=NULL WHERE name='insider'`, initial)
		exec(`DELETE FROM people_roles WHERE person_id=$1`, admin)
		exec(`DELETE FROM people WHERE id IN ($1,$2)`, admin, ordinary)
	}()
	session := scs.New()
	app := &config.AppContext{DB: pool, Session: session, Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	request := func(person, csrf, value string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/admin/site-features", strings.NewReader(url.Values{"csrf": {csrf}, "insider": {value}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx, err := session.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		r = r.WithContext(ctx)
		if person != "" {
			if err := auth.LoginPerson(app, r, person, auth.MethodPassword); err != nil {
				t.Fatal(err)
			}
		}
		session.Put(ctx, authMethodsCSRFKey, "feature-csrf")
		w := httptest.NewRecorder()
		AdminSiteFeatures(w, r, app)
		return w
	}
	exec(`UPDATE site_features SET enabled=false WHERE name='insider'`)
	if w := request(ordinary, "feature-csrf", "on"); (w.Code != 403 && w.Code != 303) || w.Header().Get("Location") == "/admin/site-features?saved=1" {
		t.Fatalf("ordinary account: %d", w.Code)
	}
	if w := request(admin, "incorrect", "on"); w.Code != 403 {
		t.Fatalf("CSRF: %d", w.Code)
	}
	if insiderVisible(app) {
		t.Fatal("rejected requests changed flag")
	}
	if w := request(admin, "feature-csrf", "on"); w.Code != 303 {
		t.Fatalf("enable: %d", w.Code)
	}
	if !insiderVisible(app) {
		t.Fatal("enabled flag not persisted")
	}
	if w := request(admin, "feature-csrf", ""); w.Code != 303 {
		t.Fatalf("disable: %d", w.Code)
	}
	if insiderVisible(app) {
		t.Fatal("disabled flag not persisted")
	}
}
