package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
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
	app := &config.AppContext{DB: pool, Err: log.New(os.Stderr, "prepaid test: ", 0), Session: scs.New(), Env: &types.EnvConfig{StripeKey: "sk_test_local_fixture"}}
	app.Env.OpenNode.Key = "local-fixture"
	app.TemplateCache, err = template.ParseFiles("../../templates/alerts_bitcoin_start.tmpl", "../../templates/service_quote.tmpl", "../../templates/service_order.tmpl")
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
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, user, user+"@example.test"); err != nil {
		t.Fatal(err)
	}
	chargeID := uuid.NewString()
	calculationID := "taxcalc_" + strings.ReplaceAll(user, "-", "")
	var attemptID atomic.Value
	var paid atomic.Bool
	var creates atomic.Int32
	taxCalls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tax/transactions/create_from_calculation" && r.Method == "POST" {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("calculation") != calculationID || !strings.HasPrefix(r.Form.Get("reference"), "service-order-") || r.Header.Get("Idempotency-Key") == "" {
				t.Error("incorrect tax finalization", r.Form)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "tax_" + strings.ReplaceAll(user, "-", ""), "type": "transaction", "currency": "usd", "reference": r.Form.Get("reference"), "line_items": map[string]any{"data": []any{map[string]any{"amount": 4800, "amount_tax": 200, "quantity": 2, "reference": "alerts.repository-slots", "tax_behavior": "inclusive"}}}})
			return
		}
		if r.URL.Path == "/v1/charges" && r.Method == "POST" {
			creates.Add(1)
			if r.Header.Get("Authorization") != "local-fixture" {
				t.Error("missing provider authentication")
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload["amount"] != "48.00" || payload["currency"] != "USD" || payload["customer_email"] != user+"@example.test" {
				t.Error("untrusted charge terms", payload)
			}
			attemptID.Store(payload["order_id"].(string))
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": chargeID, "order_id": attemptID.Load()}})
			return
		}
		if r.URL.Path == "/v2/charge/"+chargeID && r.Method == "GET" {
			if r.Header.Get("Authorization") != "local-fixture" {
				t.Error("missing lookup authentication")
			}
			status := "unpaid"
			if paid.Load() {
				status = "paid"
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": chargeID, "order_id": attemptID.Load(), "status": status, "currency": "USD", "source_fiat_value": 48, "price": 48000, "fee": 480, "missing_amt": 0, "hosted_checkout_url": "https://checkout.opennode.com/" + chargeID}})
			return
		}
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
		json.NewEncoder(w).Encode(map[string]any{"id": calculationID, "currency": "usd", "amount_total": 4800, "tax_amount_inclusive": 200, "tax_amount_exclusive": 0, "expires_at": time.Now().Add(time.Hour).Unix(), "line_items": map[string]any{"data": []any{map[string]any{"amount": 4800, "amount_tax": 200, "quantity": 2, "reference": "alerts.repository-slots", "tax_behavior": "inclusive", "tax_code": "txcd_10103000"}}}})
	}))
	defer provider.Close()
	app.Env.OpenNode.Endpoint = provider.URL + "/v1"
	previous, key := stripe.GetBackend(stripe.APIBackend), stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	stripe.Key = app.Env.StripeKey
	defer func() { stripe.SetBackend(stripe.APIBackend, previous); stripe.Key = key }()
	router := mux.NewRouter()
	router.HandleFunc("/login-fixture", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, user, auth.MethodPassword); err != nil {
			t.Error(err)
		}
		w.WriteHeader(204)
	})
	router.HandleFunc("/services/alerts/prepay", func(w http.ResponseWriter, r *http.Request) { AlertsBitcoinStart(w, r, app) })
	router.HandleFunc("/services/quotes/{quote}", func(w http.ResponseWriter, r *http.Request) { ServiceQuoteReview(w, r, app) }).Methods("GET")
	router.HandleFunc("/services/quotes/{quote}/confirm", func(w http.ResponseWriter, r *http.Request) { ServiceQuoteConfirm(w, r, app) }).Methods("POST")
	router.HandleFunc("/dashboard/orders/{order}", func(w http.ResponseWriter, r *http.Request) { DashboardOrder(w, r, app) }).Methods("GET")
	router.HandleFunc("/dashboard/orders/{order}/receipt", func(w http.ResponseWriter, r *http.Request) { DashboardOrderReceipt(w, r, app) }).Methods("GET")
	router.HandleFunc("/services/orders/{order}", func(w http.ResponseWriter, r *http.Request) { ServiceOrder(w, r, app) }).Methods("GET")
	router.HandleFunc("/services/orders/{order}/pay", func(w http.ResponseWriter, r *http.Request) { ServiceOrderPay(w, r, app) }).Methods("POST")
	router.HandleFunc("/callback/services/opennode", func(w http.ResponseWriter, r *http.Request) { ServiceOpenNodeCallback(w, r, app) }).Methods("POST")
	server := httptest.NewServer(app.Session.LoadAndSave(router))
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
	policyVersion := "bitcoin-policy-" + user
	policy := getters.ServiceLegalPolicy{ProductCode: servicebilling.AlertsProduct, Version: policyVersion, TermsText: "Reviewed prepaid <terms>", PrivacyText: "Reviewed prepaid privacy", EffectiveAt: time.Now().Add(-time.Hour).UTC().Truncate(time.Second)}
	if err := getters.RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err := getters.ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM service_legal_current WHERE product_code=$1`, policy.ProductCode)
	quotePath := location
	code, body, _ = call(quotePath, nil)
	if code != 200 || !strings.Contains(body, "$48.00") || !strings.Contains(body, "$2.00") {
		t.Fatal("quote review", code, body)
	}
	csrf = regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(csrf) != 2 {
		t.Fatal(body)
	}
	confirm := url.Values{"csrf": {csrf[1]}, "quantity": {"999"}, "price": {"1"}}
	if !strings.Contains(body, "Reviewed prepaid &lt;terms&gt;") || !strings.Contains(body, `name="accept_terms" value="yes" required`) {
		t.Fatal("missing policy review", body)
	}
	if code, _, _ := call(quotePath+"/confirm", confirm); code != 409 {
		t.Fatal("missing agreement created order", code)
	}
	confirm.Set("accept_terms", "yes")
	confirm.Set("legal_version", "stale")
	if code, _, _ := call(quotePath+"/confirm", confirm); code != 409 {
		t.Fatal("stale agreement created order", code)
	}
	confirm.Set("legal_version", policyVersion)
	// Choosing Bitcoin automatically closes an unsubmitted card reservation.
	pendingCard, err := getters.ReserveAlertsStripeCheckoutWithPolicy(app, user, "bitcoin-conflict-"+user, 2, "https://btcpp.dev", policyVersion)
	if err != nil {
		t.Fatal(err)
	}
	confirm.Set("legal_version", "stale")
	if code, _, _ := call(quotePath+"/confirm", confirm); code != 409 {
		t.Fatal("stale policy switched payment methods", code)
	}
	pending, err := getters.AlertsStripeCheckoutForPerson(app, user, pendingCard.Intent.ID)
	if err != nil || pending.Closed {
		t.Fatal("invalid confirmation cancelled card checkout", pending, err)
	}
	confirm.Set("legal_version", policyVersion)
	code, body, orderPath := call(quotePath+"/confirm", confirm)
	if code != 303 || !strings.HasPrefix(orderPath, "/services/orders/") {
		t.Fatal("confirm", code, body, orderPath)
	}
	closed, err := getters.AlertsStripeCheckoutForPerson(app, user, pendingCard.Intent.ID)
	if err != nil || !closed.Closed {
		t.Fatal("card checkout was not cancelled", closed, err)
	}
	// Re-entering checkout resumes the same order without recalculating tax,
	// starting a charge, or accepting new browser-supplied purchase terms.
	for _, retryForm := range []url.Values{nil, form} {
		if code, _, resume := call(path, retryForm); code != 303 || resume != orderPath {
			t.Fatal("prepay did not resume pending order", code, resume)
		}
	}
	if code, _, _ := call(strings.Replace(path, user, other, 1), nil); code != 403 {
		t.Fatal("pending order bypassed account check", code)
	}
	form.Set("csrf", "wrong")
	if code, _, _ := call(path, form); code != 403 {
		t.Fatal("pending order bypassed CSRF", code)
	}
	form.Set("csrf", csrf[1])
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM service_purchase_requests WHERE person_id=$1`, user).Scan(&orders); err != nil || orders != 1 || taxCalls != 1 || creates.Load() != 0 {
		t.Fatal("resume created another purchase or provider request", orders, taxCalls, creates.Load(), err)
	}
	// Pending service orders must reach the verified service view, not a generic
	// shop PDF that could imply payment was received.
	for _, suffix := range []string{"", "/receipt"} {
		path := strings.Replace(orderPath, "/services/orders/", "/dashboard/orders/", 1) + suffix
		if code, _, location := call(path, nil); code != 303 || location != orderPath {
			t.Fatal("service receipt routing", code, location)
		}
	}
	if code, _, repeat := call(quotePath+"/confirm", confirm); code != 303 || repeat != orderPath {
		t.Fatal("repeat confirmation", code, repeat)
	}
	if code, _, resume := call(quotePath, nil); code != 303 || resume != orderPath {
		t.Fatal("confirmed quote did not resume original order", code, resume)
	}
	code, body, _ = call(orderPath, nil)
	if code != 200 || !strings.Contains(body, "Pay $48.00 with Bitcoin") || !strings.Contains(body, "Accepted policy version") || !strings.Contains(body, policyVersion) || !strings.Contains(body, "Reviewed prepaid &lt;terms&gt;") {
		t.Fatal("order", code, body)
	}
	csrf = regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(csrf) != 2 {
		t.Fatal(body)
	}
	for i := 0; i < 2; i++ {
		code, body, destination := call(orderPath+"/pay", url.Values{"csrf": {csrf[1]}, "amount": {"1"}})
		if code != 303 || destination != "https://checkout.opennode.com/"+chargeID {
			t.Fatal("payment handoff", code, body, destination)
		}
	}
	if creates.Load() != 1 {
		t.Fatal("duplicate charges", creates.Load())
	}
	callback := url.Values{"id": {chargeID}, "hashed_order": {computeHash(app.Env.OpenNode.Key, chargeID)}, "status": {"paid"}, "price": {"1"}}
	if code, body, _ := call("/callback/services/opennode", callback); code != 204 {
		t.Fatal(code, body)
	}
	access, err := getters.AccountAccess(ctx, app, user, time.Now())
	if err != nil || len(access.Entitlements) != 0 {
		t.Fatal("unpaid provider granted access", access, err)
	}
	paid.Store(true)
	for i := 0; i < 2; i++ {
		if code, body, _ := call("/callback/services/opennode", callback); code != 204 {
			t.Fatal(code, body)
		}
	}
	access, err = getters.AccountAccess(ctx, app, user, time.Now())
	if err != nil || len(access.Entitlements) != 1 {
		t.Fatal("missing or duplicated grant", access, err)
	}
	grant := access.Entitlements[0]
	if grant.ProductCode != "alerts.repository-slots" || grant.Quantity != 2 || grant.StartsAt == nil || grant.EndsAt == nil || !grant.StartsAt.Equal(*stored.Quote.PeriodStart) || !grant.EndsAt.Equal(*stored.Quote.PeriodEnd) {
		t.Fatal("access differs from paid quote", grant)
	}
	code, body, _ = call(orderPath, nil)
	if code != 200 || !strings.Contains(body, "Your purchased access is active") || strings.Contains(body, "Pay $48.00 with Bitcoin") {
		t.Fatal("paid receipt", code, body)
	}
	var grants, events int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM service_entitlements WHERE person_id=$1),(SELECT count(*) FROM service_payment_events p JOIN service_payment_attempts a ON a.id=p.attempt_id JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.person_id=$1)`, user).Scan(&grants, &events); err != nil || grants != 1 || events != 1 {
		t.Fatal("duplicate settlement", grants, events, err)
	}
	var orderID string
	var receiptJobs int
	if err = pool.QueryRow(ctx, `SELECT o.id::text,(SELECT count(*) FROM service_receipt_jobs j WHERE j.order_id=o.id) FROM shop_orders o WHERE o.public_id=$1`, strings.TrimPrefix(orderPath, "/services/orders/")).Scan(&orderID, &receiptJobs); err != nil || receiptJobs != 1 {
		t.Fatal("missing or duplicate receipt job", receiptJobs, err)
	}
	var mailCalls atomic.Int32
	var firstKey string
	mailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		// The mailer API uses its shared request serialization and stable job key.
		raw, _ := json.Marshal(payload)
		if r.Method != "PUT" || r.URL.Path != "/job" || !strings.Contains(string(raw), "service-receipt:") || !strings.Contains(string(raw), "$48.00") || !strings.Contains(string(raw), user+"@example.test") {
			t.Error("invalid receipt request", r.Method, r.URL.Path, string(raw))
		}
		key := ""
		for name, value := range payload {
			if strings.EqualFold(name, "jobkey") || strings.EqualFold(name, "job_key") {
				key, _ = value.(string)
			}
		}
		if firstKey == "" {
			firstKey = key
		} else if key != firstKey {
			t.Error("receipt retry changed identity")
		}
		if mailCalls.Add(1) == 1 {
			http.Error(w, "temporary failure", 503)
			return
		}
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer mailServer.Close()
	app.Env.MailEndpoint = mailServer.URL
	app.Env.LocalExternal = "https://btcpp.dev"
	app.Infos = log.New(io.Discard, "", 0)
	at := time.Now().Add(time.Second)
	app.Env.MailOff = true
	if worked, err := processServiceReceipt(ctx, app, orderID, at); err != nil || worked {
		t.Fatal("disabled mail consumed job", worked, err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, "SELECT attempts FROM service_receipt_jobs WHERE order_id=$1", orderID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("disabled mail leased job", attempts, err)
	}
	app.Env.MailOff = false
	if worked, err := processServiceReceipt(ctx, app, orderID, at); err == nil || !worked {
		t.Fatal("missing mail retry", worked, err)
	}
	if worked, err := processServiceReceipt(ctx, app, orderID, at.Add(time.Minute)); err != nil || worked {
		t.Fatal("ignored receipt backoff", worked, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := processServiceReceipt(ctx, app, orderID, at.Add(6*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var receiptState string
	if err := pool.QueryRow(ctx, "SELECT state FROM service_receipt_jobs WHERE order_id=$1", orderID).Scan(&receiptState); err != nil || receiptState != "accepted" || mailCalls.Load() != 2 || firstKey == "" {
		t.Fatal("receipt acceptance or deduplication", receiptState, mailCalls.Load(), firstKey, err)
	}
}
