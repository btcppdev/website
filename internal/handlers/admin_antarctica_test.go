package handlers

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func enableAntarcticaSalesForTest(t *testing.T, app *config.AppContext) func() {
	t.Helper()
	var old bool
	err := app.DB.QueryRow(context.Background(), `SELECT enabled FROM site_features WHERE name=$1`, antarcticaSalesFeature).Scan(&old)
	existed := err == nil
	if _, err := app.DB.Exec(context.Background(), `INSERT INTO site_features(name,enabled) VALUES($1,true) ON CONFLICT(name) DO UPDATE SET enabled=true`, antarcticaSalesFeature); err != nil {
		t.Fatal(err)
	}
	return func() {
		var err error
		if existed {
			_, err = app.DB.Exec(context.Background(), `UPDATE site_features SET enabled=$2,updated_by=NULL WHERE name=$1`, antarcticaSalesFeature, old)
		} else {
			_, err = app.DB.Exec(context.Background(), `DELETE FROM site_features WHERE name=$1`, antarcticaSalesFeature)
		}
		if err != nil {
			t.Error(err)
		}
	}
}

func TestAntarcticaAdminPostgres(t *testing.T) {
	uri := os.Getenv("EXPEDITION_COMMERCE_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	app := antarcticaTestTemplates(t)
	app.Infos = app.Err
	pool, err := pgxpool.New(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app.DB = pool
	app.Session = scs.New()
	defer enableAntarcticaSalesForTest(t, app)()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), q, args...); err != nil {
			t.Fatal(err)
		}
	}
	admin, ordinary := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO people(id,name) VALUES($1,'Expedition admin'),($2,'Ordinary traveler')`, admin, ordinary)
	exec(`INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, admin)
	defer func() {
		exec(`UPDATE site_features SET updated_by=NULL WHERE updated_by=$1`, admin)
		exec(`DELETE FROM people_roles WHERE person_id=$1`, admin)
		exec(`DELETE FROM people WHERE id IN ($1,$2)`, admin, ordinary)
	}()
	current, err := getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
	if err != nil {
		t.Fatal(err)
	}
	exec(`DELETE FROM service_legal_current WHERE product_code=$1`, expedition.ProductCode)
	defer func() {
		exec(`DELETE FROM service_legal_current WHERE product_code=$1`, expedition.ProductCode)
		if current != nil {
			exec(`INSERT INTO service_legal_current(product_code,version) VALUES($1,$2)`, expedition.ProductCode, current.Version)
		}
	}()
	request := func(person, csrf string, v url.Values) *httptest.ResponseRecorder {
		t.Helper()
		v.Set("csrf", csrf)
		r := httptest.NewRequest("POST", "/admin/antarctica", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx, err := app.Session.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		r = r.WithContext(ctx)
		if person != "" {
			if err := auth.LoginPerson(app, r, person, auth.MethodPassword); err != nil {
				t.Fatal(err)
			}
		}
		app.Session.Put(ctx, authMethodsCSRFKey, "test-csrf")
		w := httptest.NewRecorder()
		AdminAntarctica(w, r, app)
		return w
	}
	action := func(a string) url.Values { return url.Values{"action": {a}} }
	for _, person := range []string{"", ordinary} {
		if w := request(person, "test-csrf", action("pause")); w.Code == 303 && strings.HasPrefix(w.Header().Get("Location"), "/admin/antarctica?flash=") {
			t.Fatal("unauthorized mutation")
		}
	}
	if w := request(admin, "wrong", action("pause")); w.Code != 403 {
		t.Fatalf("csrf %d", w.Code)
	}
	if !antarcticaSalesEnabled(app) {
		t.Fatal("rejected request paused sales")
	}
	if w := request(admin, "test-csrf", action("pause")); w.Code != 303 {
		t.Fatalf("pause %d", w.Code)
	}
	if antarcticaSalesEnabled(app) {
		t.Fatal("pause not immediate")
	}
	if w := request(admin, "test-csrf", action("open")); w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "blocked=1") {
		t.Fatalf("unconfigured opening %d", w.Code)
	}
	version := uuid.NewString()
	form := url.Values{"action": {"publish"}, "version": {version}, "effective": {time.Now().UTC().Add(-time.Second).Format(time.RFC3339)}, "terms": {expedition.BookingTerms}, "privacy": {expedition.BookingPrivacy}, "reviewed": {"yes"}}
	if w := request(admin, "test-csrf", form); w.Code != 303 {
		t.Fatalf("publish %d: %s", w.Code, w.Body.String())
	}
	p, err := getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
	if err != nil || p == nil || p.Version != version {
		t.Fatalf("policy not published %v", err)
	}
	var zone string
	if err := pool.QueryRow(context.Background(), `SELECT time_zone FROM expedition_lifecycle_policies WHERE version=$1`, version).Scan(&zone); err != nil || zone != "America/Chicago" {
		t.Fatalf("lifecycle %s %v", zone, err)
	}
	form.Set("terms", "changed terms")
	if w := request(admin, "test-csrf", form); w.Code != 409 {
		t.Fatalf("immutable policy %d", w.Code)
	}
	form.Set("version", uuid.NewString())
	form.Set("effective", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	if w := request(admin, "test-csrf", form); w.Code != 409 {
		t.Fatalf("future policy %d", w.Code)
	}
	p, _ = getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
	if p.Version != version {
		t.Fatal("failed publish changed current")
	}
	for _, key := range []string{"SERVICE_FULFILLMENT_ENABLED", "SERVICE_PAYMENT_RECONCILIATION_ENABLED", "SERVICE_RECEIPTS_ENABLED", "ANTARCTICA_INSTALLMENT_LIFECYCLE_ENABLED"} {
		t.Setenv(key, "true")
	}
	app.Env.MailEndpoint = "http://never-contacted.invalid"
	app.Env.OpenNode.Key = "fixture"
	if w := request(admin, "test-csrf", action("inventory")); w.Code != 303 {
		t.Fatalf("inventory %d", w.Code)
	}
	if w := request(admin, "test-csrf", action("open")); w.Code != 303 {
		t.Fatalf("open %d: %s", w.Code, w.Body.String())
	}
	if !antarcticaCheckoutReady(app) {
		t.Fatal("open not effective")
	}
	if w := request(admin, "test-csrf", action("pause")); w.Code != 303 {
		t.Fatalf("pause %d", w.Code)
	}
	if antarcticaCheckoutReady(app) {
		t.Fatal("paused checkout ready")
	}
	if !antarcticaPaymentsReady(app) {
		t.Fatal("pausing new sales must not block installment payments")
	}
	r := httptest.NewRequest("GET", "/admin/antarctica", nil)
	ctx, err := app.Session.Load(r.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	r = r.WithContext(ctx)
	if err := auth.LoginPerson(app, r, admin, auth.MethodPassword); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	AdminAntarctica(w, r, app)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Publish booking policy") || !strings.Contains(w.Body.String(), version) || !strings.Contains(w.Body.String(), "Open Antarctica checkout") {
		t.Fatalf("admin page %d: %s", w.Code, w.Body.String())
	}
}
