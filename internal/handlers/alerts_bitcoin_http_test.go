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
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v86"
)

func TestAlertsBitcoinHTTPPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated migrated database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{StripeKey: "sk_test_local_fixture"}}
	app.Env.OpenNode.Key = "local-fixture"
	app.TemplateCache, err = template.ParseFiles("../../templates/alerts_bitcoin_start.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var user, other string
	for _, target := range []*string{&user, &other} {
		if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Alerts prepaid fixture') RETURNING id::text`).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM people WHERE id=$1 OR id=$2`, user, other); err != nil {
			t.Error(err)
		}
	}()
	taxCalls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/tax/calculations" {
			t.Error("unexpected provider operation", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		taxCalls++
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("line_items[0][amount]") != "4800" || r.Form.Get("line_items[0][quantity]") != "2" || r.Form.Get("line_items[0][tax_behavior]") != "inclusive" || r.Form.Get("line_items[0][tax_code]") != "txcd_10103000" {
			t.Error("incorrect tax request", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "taxcalc_prepaid_fixture", "currency": "usd", "amount_total": 4800, "tax_amount_inclusive": 200, "tax_amount_exclusive": 0, "expires_at": time.Now().Add(time.Hour).Unix(), "line_items": map[string]any{"data": []any{map[string]any{"amount": 4800, "amount_tax": 200, "quantity": 2, "reference": "alerts.repository-slots", "tax_behavior": "inclusive", "tax_code": "txcd_10103000"}}}})
	}))
	defer provider.Close()
	previous, key := stripe.GetBackend(stripe.APIBackend), stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	stripe.Key = app.Env.StripeKey
	defer func() { stripe.SetBackend(stripe.APIBackend, previous); stripe.Key = key }()
	mux := http.NewServeMux()
	mux.HandleFunc("/login-fixture", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, user, auth.MethodPassword); err != nil {
			t.Error(err)
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("/services/alerts/prepay", func(w http.ResponseWriter, r *http.Request) { AlertsBitcoinStart(w, r, app) })
	server := httptest.NewServer(app.Session.LoadAndSave(mux))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(path string, form url.Values) (int, string, string) {
		t.Helper()
		var res *http.Response
		var err error
		if form == nil {
			res, err = client.Get(server.URL + path)
		} else {
			res, err = client.PostForm(server.URL+path, form)
		}
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, string(raw), res.Header.Get("Location")
	}
	path := "/services/alerts/prepay?account=" + user + "&slots=2&months=12"
	if code, _, location := call(path, nil); code != 303 || !strings.Contains(location, "months%3D12") {
		t.Fatal(code, location)
	}
	call("/login-fixture", nil)
	if code, _, _ := call(strings.Replace(path, user, other, 1), nil); code != 403 {
		t.Fatal("foreign account", code)
	}
	code, body, _ := call(path, nil)
	if code != 200 || !strings.Contains(body, "$48.00") {
		t.Fatal(code, body)
	}
	csrf := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(csrf) != 2 {
		t.Fatal(body)
	}
	form := url.Values{"account": {user}, "slots": {"2"}, "months": {"12"}, "csrf": {"wrong"}, "line1": {"1 Main St"}, "line2": {""}, "city": {"Austin"}, "region": {"TX"}, "postal": {"78701"}, "country": {"US"}, "total": {"1"}, "tax": {"0"}}
	if code, _, _ := call(path, form); code != 403 {
		t.Fatal(code)
	}
	form.Set("csrf", csrf[1])
	form.Set("account", other)
	if code, _, _ := call(path, form); code != 403 {
		t.Fatal(code)
	}
	form.Set("account", user)
	if taxCalls != 0 {
		t.Fatal("unauthorized tax request")
	}
	code, body, location := call("/services/alerts/prepay?account="+other+"&slots=999&months=120", form)
	if code != 303 || !strings.HasPrefix(location, "/services/quotes/") {
		t.Fatal(code, body, location)
	}
	stored, err := getters.ServiceCheckoutQuoteForPerson(app, user, strings.TrimPrefix(location, "/services/quotes/"))
	if err != nil || stored.Quote.TotalCents != 4800 || stored.Quote.TaxCents != 200 || stored.Quote.Provider != "opennode" || stored.Quote.BillingMonths != 12 || stored.Quote.Quantity != 2 {
		t.Fatal(stored, err)
	}
	var orders int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM service_purchase_requests WHERE person_id=$1`, user).Scan(&orders); err != nil || orders != 0 || taxCalls != 1 {
		t.Fatal("quote started payment", orders, taxCalls, err)
	}
}
