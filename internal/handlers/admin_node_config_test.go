package handlers

import (
	"bytes"
	"context"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/prizepool"
	"btcpp-web/internal/types"
)

func TestNodeConfigRequiresExplicitAccountsAdmin(t *testing.T) {
	for _, role := range []string{"", "global-admin", "berlin26-admin", "merch-admin"} {
		id := &auth.Identity{Roles: auth.ParseRoles([]string{role})}
		for _, method := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			serveNodeConfig(w, httptest.NewRequest(method, "/admin/node-config", nil), &config.AppContext{}, id)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s %s returned %d", role, method, w.Code)
			}
		}
	}
	if !canManageNodeConfig(&auth.Identity{Roles: auth.ParseRoles([]string{auth.AccountsAdminTag})}) {
		t.Fatal("accounts admin rejected")
	}
	if canManageNodeConfig(nil) {
		t.Fatal("anonymous allowed")
	}
}
func TestNodeConfigTemplateAndVisibility(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err = loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	c := prizepool.DefaultNodeConfig()
	c.Host = "node.example:9735"
	c.Rune = "never-emit-monitor"
	c.ProvisionRune = "never-emit-provision"
	c.CFToken = "never-emit-cloudflare"
	page := nodeConfigView(c)
	page.CSRF = "test-csrf"
	var out bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&out, "admin/node_config.tmpl", page); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{c.Rune, c.ProvisionRune, c.CFToken} {
		if strings.Contains(out.String(), secret) {
			t.Fatal("credential exposed in HTML")
		}
	}
	for _, expected := range []string{"node.example:9735", "test-csrf", "Test saved connection", "Save configuration", "Create the service runes", "Copy monitoring command", "Copy provisioning command", "method=waitanyinvoice", "method=clnurl-remove"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	for _, allowed := range []bool{false, true} {
		out.Reset()
		if err = app.TemplateCache.ExecuteTemplate(&out, "dashboard_person_emails.tmpl", &PersonEmailsPage{IsGlobalAdmin: true, IsAccountsAdmin: allowed}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "/admin/node-config") != allowed {
			t.Fatal("account settings link ignored explicit role")
		}
		out.Reset()
		if err = app.TemplateCache.ExecuteTemplate(&out, "admin/dashboard.tmpl", &GlobalAdminDashboardPage{IsAccountsAdmin: allowed}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "/admin/node-config") != allowed {
			t.Fatal("global admin link ignored explicit role")
		}
	}
	if os.Getenv("NODE_CONFIG_PREVIEW") == "1" {
		mux := http.NewServeMux()
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
		mux.HandleFunc("/admin/node-config", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "Preview only", 405)
				return
			}
			p := page
			if r.URL.Query().Get("empty") == "1" {
				p = nodeConfigView(prizepool.DefaultNodeConfig())
			}
			if err := app.TemplateCache.ExecuteTemplate(w, "admin/node_config.tmpl", p); err != nil {
				t.Error(err)
			}
		})
		t.Log("Node configuration preview: http://127.0.0.1:8107/admin/node-config")
		t.Fatal(http.ListenAndServe("127.0.0.1:8107", mux))
	}
}

func TestNodeConfigFormPersistenceAndCSRF(t *testing.T) {
	connection := os.Getenv("PRIZE_TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	schema := "node_form_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = db.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	isolated, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { isolated.Close(); db.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); db.Close() }()
	if _, err = isolated.Exec(ctx, `CREATE TABLE community_prize_pools(domain text,node_id text)`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../db/migrations/106_community_node_config.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	session := scs.New()
	requestCtx, err := session.Load(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	session.Put(requestCtx, authMethodsCSRFKey, "form-csrf")
	app := &config.AppContext{DB: isolated, Session: session, Env: &types.EnvConfig{HMACSecret: "test root secret"}}
	// Successful POSTs redirect before rendering; error paths use a deliberately
	// tiny template to exercise persistence without global template setup.
	app.TemplateCache = template.Must(template.New("admin/node_config.tmpl").Parse(`{{.Error}} {{.Message}}`))
	id := &auth.Identity{PersonID: "accounts-admin-person", Roles: auth.ParseRoles([]string{auth.AccountsAdminTag})}
	form := url.Values{"csrf": {"form-csrf"}, "revision": {"0"}, "action": {"save"}, "host": {"node.example:9735"}, "node_id": {"02" + strings.Repeat("ab", 32)}, "network": {"bitcoin"}, "domain": {"zap.btcplusplus.dev"}, "monitor_rune": {"observer-secret"}, "provision_rune": {"provision-secret"}, "cf_token": {"dns-secret"}, "enabled": {"yes"}}
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/admin/node-config", strings.NewReader(form.Encode())).WithContext(requestCtx)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		serveNodeConfig(w, req, app, id)
		return w
	}
	form.Set("csrf", "wrong")
	if w := post(); w.Code != 403 {
		t.Fatalf("bad CSRF status %d", w.Code)
	}
	c, err := prizepool.LoadNodeConfig(ctx, isolated, app.Env.HMACSecret)
	if err != nil || c.Revision != 0 {
		t.Fatal("bad CSRF mutated settings")
	}
	form.Set("csrf", "form-csrf")
	if w := post(); w.Code != 303 {
		t.Fatalf("save status %d: %s", w.Code, w.Body.String())
	}
	c, err = prizepool.LoadNodeConfig(ctx, isolated, app.Env.HMACSecret)
	if err != nil || !c.Active || c.Rune != "observer-secret" {
		t.Fatal("form not persisted")
	}
	if w := post(); w.Code != 409 {
		t.Fatal("stale form accepted")
	}
	form.Set("revision", "1")
	form.Set("monitor_rune", "")
	form.Set("provision_rune", "")
	form.Set("cf_token", "")
	if w := post(); w.Code != 303 {
		t.Fatalf("blank-preserve status %d", w.Code)
	}
	c, err = prizepool.LoadNodeConfig(ctx, isolated, app.Env.HMACSecret)
	if err != nil || c.Rune != "observer-secret" || c.ProvisionRune != "provision-secret" || c.CFToken != "dns-secret" {
		t.Fatal("blank fields erased saved secrets")
	}
	form.Set("revision", "2")
	form.Set("monitor_rune", "replacement-secret")
	form.Set("clear_cf_token", "yes")
	if w := post(); w.Code != 303 {
		t.Fatalf("rotation status %d", w.Code)
	}
	c, err = prizepool.LoadNodeConfig(ctx, isolated, app.Env.HMACSecret)
	if err != nil || c.Rune != "replacement-secret" || c.CFToken != "" {
		t.Fatal("rotation/clear failed")
	}
	form.Set("revision", "3")
	form.Set("enabled", "")
	form.Set("clear_monitor_rune", "yes")
	form.Set("clear_provision_rune", "yes")
	if w := post(); w.Code != 303 {
		t.Fatalf("disable status %d", w.Code)
	}
	c, err = prizepool.LoadNodeConfig(ctx, isolated, app.Env.HMACSecret)
	if err != nil || c.Active || c.Rune != "" || c.ProvisionRune != "" {
		t.Fatal("disable/remove failed")
	}
	form.Set("revision", "4")
	form.Set("action", "test")
	if w := post(); !strings.Contains(w.Body.String(), "monitoring rune first") {
		t.Fatal("connection check did not require saved credentials")
	}
}
