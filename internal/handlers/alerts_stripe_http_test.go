package handlers

import (
	"context"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v86"
)

// Real account sessions, SQL and Stripe SDK HTTP serialization; the provider is
// a local fixture, so this never creates a real Stripe checkout or charge.
func TestAlertsStripeHTTPPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated migrated database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{Host: "localhost", Port: "8898", StripeKey: "sk_test_local_fixture"}}
	app.TemplateCache, err = template.ParseFiles("../../templates/alerts_stripe_start.tmpl", "../../templates/alerts_stripe_return.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var people []string
	defer func() {
		for _, id := range people {
			if _, err := pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, id); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < 2; i++ {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Alerts HTTP fixture') RETURNING id::text`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		people = append(people, id)
		if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, id, id+"@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	var lock sync.Mutex
	creates, reads := 0, 0
	var session map[string]any
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/v1/checkout/sessions" {
			creates++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("line_items[0][quantity]") != "2" || r.Form.Get("line_items[0][price_data][unit_amount]") != "200" || r.Form.Get("line_items[0][price_data][tax_behavior]") != "inclusive" || r.Header.Get("Idempotency-Key") == "" {
				t.Error("incorrect Stripe request", r.Form)
			}
			intent := r.Form.Get("client_reference_id")
			session = map[string]any{"id": "cs_fixture", "object": "checkout.session", "mode": "subscription", "status": "open", "livemode": false, "expires_at": time.Now().Add(time.Hour).Unix(), "client_reference_id": intent, "metadata": map[string]string{"account_id": people[0], "checkout_intent": intent, "product": "alerts.repository-slots"}, "url": "https://checkout.stripe.com/c/pay/cs_fixture"}
		} else if r.Method == "GET" && r.URL.Path == "/v1/checkout/sessions/cs_fixture" {
			reads++
		} else {
			t.Error("unexpected provider request", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(session)
	}))
	defer provider.Close()
	previous, key := stripe.GetBackend(stripe.APIBackend), stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	stripe.Key = app.Env.StripeKey
	defer func() { stripe.SetBackend(stripe.APIBackend, previous); stripe.Key = key }()
	router := mux.NewRouter()
	router.HandleFunc("/fixture/login/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, mux.Vars(r)["id"], auth.MethodPassword); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})
	router.HandleFunc("/services/alerts/subscribe", func(w http.ResponseWriter, r *http.Request) { AlertsStripeStart(w, r, app) })
	router.HandleFunc("/services/alerts/checkout/{checkout}", func(w http.ResponseWriter, r *http.Request) { AlertsStripeReturn(w, r, app) })
	server := httptest.NewServer(app.Session.LoadAndSave(router))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(path string, form url.Values) (int, string, string) {
		t.Helper()
		var response *http.Response
		var err error
		if form == nil {
			response, err = client.Get(server.URL + path)
		} else {
			response, err = client.PostForm(server.URL+path, form)
		}
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(raw), response.Header.Get("Location")
	}
	path := "/services/alerts/subscribe?account=" + people[0] + "&slots=2"
	code, _, location := call(path, nil)
	if code != 303 || !strings.Contains(location, "slots%3D2") {
		t.Fatal("login lost selection", code, location)
	}
	if code, body, _ := call("/fixture/login/"+people[0], nil); code != 204 {
		t.Fatal(code, body)
	}
	if code, _, _ := call("/services/alerts/subscribe?account="+people[1]+"&slots=2", nil); code != 403 {
		t.Fatal("cross-account start", code)
	}
	code, html, _ := call(path, nil)
	if code != 200 {
		t.Fatal(code, html)
	}
	field := func(name string) string {
		t.Helper()
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(html)
		if len(match) != 2 {
			t.Fatal("missing field", name, html)
		}
		return match[1]
	}
	form := url.Values{"account": {people[0]}, "slots": {"2"}, "key": {field("key")}, "csrf": {"wrong"}}
	if code, _, _ := call("/services/alerts/subscribe", form); code != 403 {
		t.Fatal("missing CSRF fence", code)
	}
	form.Set("csrf", field("csrf"))
	for i := 0; i < 2; i++ {
		code, body, location := call("/services/alerts/subscribe?slots=999&account="+people[1], form)
		if code != 303 || location != "https://checkout.stripe.com/c/pay/cs_fixture" {
			t.Fatal(code, body, location)
		}
	}
	lock.Lock()
	gotCreates, gotReads := creates, reads
	lock.Unlock()
	if gotCreates != 1 || gotReads != 1 {
		t.Fatal("duplicate checkout", gotCreates, gotReads)
	}
	var intent string
	if err = pool.QueryRow(ctx, `SELECT id::text FROM alerts_stripe_checkouts WHERE person_id=$1`, people[0]).Scan(&intent); err != nil {
		t.Fatal(err)
	}
	returnPath := "/services/alerts/checkout/" + intent
	code, body, _ := call(returnPath+"?result=returned&paid=true", nil)
	if code != 200 || !strings.Contains(body, "Awaiting payment confirmation") {
		t.Fatal("redirect claimed payment", code, body)
	}
	var grants int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM service_entitlements WHERE person_id=$1`, people[0]).Scan(&grants); err != nil || grants != 0 {
		t.Fatal(grants, err)
	}
	form.Set("slots", "3")
	if code, _, _ := call("/services/alerts/subscribe", form); code != 409 {
		t.Fatal("changed checkout quantity", code)
	}
	if code, body, _ := call("/fixture/login/"+people[1], nil); code != 204 {
		t.Fatal(code, body)
	}
	if code, _, _ := call(returnPath, nil); code != 404 {
		t.Fatal("cross-account checkout visible", code)
	}
}
