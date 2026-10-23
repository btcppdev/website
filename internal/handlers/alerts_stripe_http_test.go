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

	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
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
	app.TemplateCache, err = template.ParseFiles("../../templates/alerts_stripe_start.tmpl", "../../templates/alerts_stripe_return.tmpl", "../../templates/alerts_stripe_manage.tmpl", "../../templates/alerts_stripe_upgrade.tmpl", "../../templates/alerts_stripe_decrease.tmpl")
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
	sessionID := "cs_fixture_" + strings.ReplaceAll(people[0], "-", "")
	var lock sync.Mutex
	creates, reads := 0, 0
	var session map[string]any
	var managed *stripe.Subscription
	cancellations := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if managed != nil && r.URL.Path == "/v1/subscriptions/"+managed.ID {
			if r.Method == "POST" {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("cancel_at_period_end") != "true" || r.Header.Get("Idempotency-Key") == "" {
					t.Error("unsafe cancellation request")
				}
				cancellations++
				managed.CancelAtPeriodEnd = true
			}
			json.NewEncoder(w).Encode(managed)
			return
		}

		if r.Method == "POST" && r.URL.Path == "/v1/checkout/sessions" {
			creates++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("line_items[0][quantity]") != "2" || r.Form.Get("line_items[0][price_data][unit_amount]") != "2400" || r.Form.Get("line_items[0][price_data][tax_behavior]") != "inclusive" || r.Header.Get("Idempotency-Key") == "" {
				t.Error("incorrect Stripe request", r.Form)
			}
			intent := r.Form.Get("client_reference_id")
			session = map[string]any{"id": sessionID, "object": "checkout.session", "mode": "subscription", "status": "open", "livemode": false, "expires_at": time.Now().Add(time.Hour).Unix(), "client_reference_id": intent, "metadata": map[string]string{"account_id": people[0], "checkout_intent": intent, "product": "alerts.repository-slots"}, "url": "https://checkout.stripe.com/c/pay/" + sessionID}
		} else if r.Method == "GET" && r.URL.Path == "/v1/checkout/sessions/"+sessionID {
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
	router.HandleFunc("/services/alerts/upgrade/{quote}", func(w http.ResponseWriter, r *http.Request) { AlertsStripeUpgrade(w, r, app) })
	router.HandleFunc("/services/alerts/decrease/{quote}", func(w http.ResponseWriter, r *http.Request) { AlertsStripeDecrease(w, r, app) })
	router.HandleFunc("/services/alerts/manage", func(w http.ResponseWriter, r *http.Request) { AlertsStripeManage(w, r, app) })
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
	if code, _, location := call("/services/alerts/manage?account="+people[0], nil); code != 303 || !strings.Contains(location, "account%3D"+people[0]) {
		t.Fatal("management login lost account", code, location)
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
	if code, body, _ := call("/services/alerts/manage?account="+people[0], nil); code != 200 || !strings.Contains(body, "No card subscription") || strings.Contains(body, "Cancel future renewals") {
		t.Fatal("empty management page", code, body)
	}
	if code, _, _ := call("/services/alerts/manage?account="+people[1], nil); code != 403 {
		t.Fatal("wrong management account", code)
	}
	if code, _, _ := call("/services/alerts/manage", url.Values{"csrf": {"wrong"}, "checkout": {people[0]}, "key": {people[1]}, "revision": {"1"}, "periodEnd": {"1"}}); code != 403 {
		t.Fatal("management CSRF", code)
	}
	policyVersion := "http-policy-" + people[0]
	policy := getters.ServiceLegalPolicy{ProductCode: servicebilling.AlertsProduct, Version: policyVersion, TermsText: "Reviewed <terms>", PrivacyText: "Reviewed privacy", EffectiveAt: time.Now().UTC().Add(-time.Hour).Truncate(time.Second)}
	if err := getters.RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err := getters.ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM service_legal_current WHERE product_code=$1`, policy.ProductCode)
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
	if !strings.Contains(html, "Reviewed &lt;terms&gt;") || !strings.Contains(html, `name="accept_terms" value="yes" required`) {
		t.Fatal("review lacks escaped policy and required agreement", html)
	}
	if code, _, _ := call("/services/alerts/subscribe", form); code != 409 {
		t.Fatal("missing agreement accepted", code)
	}
	form.Set("accept_terms", "yes")
	form.Set("legal_version", "stale")
	if code, _, _ := call("/services/alerts/subscribe", form); code != 409 {
		t.Fatal("stale agreement accepted", code)
	}
	form.Set("legal_version", field("legal_version"))
	for i := 0; i < 2; i++ {
		code, body, location := call("/services/alerts/subscribe?slots=999&account="+people[1], form)
		if code != 303 || location != "https://checkout.stripe.com/c/pay/"+sessionID {
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
	lock.Lock()
	session["status"] = "expired"
	session["payment_status"] = "unpaid"
	raw, err := json.Marshal(session)
	lock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if code, body, _ := call("/fixture/login/"+people[0], nil); code != 204 {
		t.Fatal(code, body)
	}
	code, html, _ = call(returnPath, nil)
	if code != 200 {
		t.Fatal(code, html)
	}
	checkForm := url.Values{"action": {"check"}, "csrf": {"wrong"}}
	if code, _, _ := call(returnPath, checkForm); code != 403 {
		t.Fatal("recovery CSRF", code)
	}
	checkForm.Set("csrf", field("csrf"))
	if code, body, location := call(returnPath, checkForm); code != 303 || location != returnPath {
		t.Fatal(code, body, location)
	}
	if code, body, _ := call(returnPath, nil); code != 200 || !strings.Contains(body, "Checkout closed") {
		t.Fatal("missed webhook recovery", code, body)
	}
	lock.Lock()
	gotCreates = creates
	lock.Unlock()
	if gotCreates != 1 {
		t.Fatal("recovery created another payment", gotCreates)
	}
	handled, err := processAlertsStripeEvent(ctx, app, stripe.Event{Type: "checkout.session.expired", Data: &stripe.EventData{Raw: raw}}, alertsstripe.NewClient(false))
	if err != nil || !handled {
		t.Fatal("expiry callback reconciliation", handled, err)
	}
	if code, body, _ := call("/fixture/login/"+people[0], nil); code != 204 {
		t.Fatal(code, body)
	}
	if code, body, _ := call(returnPath, nil); code != 200 || !strings.Contains(body, "Checkout closed") {
		t.Fatal(code, body)
	}
	// A separate, account-owned subscription exercises the management flow.
	saved, e := getters.ReserveAlertsStripeCheckoutWithPolicy(app, people[1], "management-checkout-fixture", 2, "https://btcpp.dev", policyVersion)
	if e != nil {
		t.Fatal(e)
	}
	b := alertsstripe.Binding{IntentID: saved.Intent.ID, PersonID: people[1], SubscriptionID: "sub_" + saved.Intent.ID, CustomerID: "cus_" + people[1], PriceID: "price_" + saved.Intent.ID, ProductID: "prod_" + saved.Intent.ID, PaidSlots: 2}
	rawBinding, e := json.Marshal(b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, b.IntentID, b.SubscriptionID, rawBinding); e != nil {
		t.Fatal(e)
	}
	lock.Lock()
	managed = &stripe.Subscription{ID: b.SubscriptionID, Status: "active", Customer: &stripe.Customer{ID: b.CustomerID}, Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": "alerts.repository-slots"}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: 2, CurrentPeriodStart: time.Now().Add(-time.Hour).Unix(), CurrentPeriodEnd: time.Now().Add(24 * time.Hour).Unix(), Price: &stripe.Price{ID: b.PriceID, Currency: "usd", UnitAmount: 2400, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "year", IntervalCount: 1}, Product: &stripe.Product{ID: b.ProductID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}}}}}
	lock.Unlock()
	if code, _, _ := call("/fixture/login/"+people[1], nil); code != 204 {
		t.Fatal(code)
	}
	code, html, _ = call("/services/alerts/manage?cancelled=true", nil)
	if code != 200 || !strings.Contains(html, "Cancel future renewals") {
		t.Fatal("missing cancellation review", code, html)
	}
	cancelForm := url.Values{"checkout": {field("checkout")}, "csrf": {field("csrf")}, "key": {field("key")}, "revision": {field("revision")}, "periodEnd": {field("periodEnd")}}
	now := time.Now().UTC().Truncate(time.Second)
	quote, e := getters.SaveAlertsStripeIncreaseQuote(ctx, app, people[1], b.IntentID, 1, alertsstripe.IncreasePreview{SubscriptionItemID: "si_test", PreviousSlots: 2, PaidSlots: 3, PeriodStart: managed.Items.Data[0].CurrentPeriodStart, PeriodEnd: managed.Items.Data[0].CurrentPeriodEnd, ProrationDate: now.Unix(), TotalCents: 195, TaxCents: 15, ExpiresAt: now.Add(10 * time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	upgradePath := "/services/alerts/upgrade/" + quote.ID
	decrease, e := getters.SaveAlertsStripeDecreaseQuote(ctx, app, people[1], b.IntentID, 1, alertsstripe.DecreasePreview{PreviousSlots: 2, PaidSlots: 1, AnnualCents: 2400, PeriodStart: managed.Items.Data[0].CurrentPeriodStart, EffectiveAt: managed.Items.Data[0].CurrentPeriodEnd, ExpiresAt: now.Add(10 * time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	decreasePath := "/services/alerts/decrease/" + decrease.ID
	decreaseForm := url.Values{"checkout": {b.IntentID}, "csrf": {cancelForm.Get("csrf")}, "action": {"decrease"}, "slots": {"1"}}
	for i := 0; i < 2; i++ {
		if code, body, location := call("/services/alerts/manage", decreaseForm); code != 303 || location != decreasePath {
			t.Fatal("decrease review handoff", code, body, location)
		}
	}
	decreaseForm.Set("slots", "0")
	if code, _, _ := call("/services/alerts/manage", decreaseForm); code != 400 {
		t.Fatal("zero slots must use cancellation", code)
	}
	decreaseForm.Set("slots", "2")
	if code, _, _ := call("/services/alerts/manage", decreaseForm); code != 409 {
		t.Fatal("non-decrease accepted", code)
	}
	decreaseForm.Set("slots", "1")
	decreaseForm.Set("csrf", "wrong")
	if code, _, _ := call("/services/alerts/manage", decreaseForm); code != 403 {
		t.Fatal("decrease selection csrf", code)
	}
	code, html, _ = call(decreasePath+"?state=scheduled&total=1&slots=99", nil)
	if code != 200 || !strings.Contains(html, "$24.00") || !strings.Contains(html, "2 → 1") || !strings.Contains(html, "Confirm decrease") || strings.Contains(html, "Your decrease is scheduled") {
		t.Fatal("decrease review", code, html)
	}
	if code, _, _ = call(decreasePath, url.Values{"csrf": {"wrong"}, "action": {"confirm"}}); code != 403 {
		t.Fatal("decrease csrf", code)
	}
	if code, _, _ = call(decreasePath, url.Values{"csrf": {field("csrf")}, "action": {"unknown"}}); code != 400 {
		t.Fatal("decrease action", code)
	}
	var confirmedDecreases int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_decreases WHERE quote_id=$1`, decrease.ID).Scan(&confirmedDecreases); e != nil || confirmedDecreases != 0 {
		t.Fatal("GET confirmed decrease", confirmedDecreases, e)
	}
	if code, _, _ := call(decreasePath, url.Values{"csrf": {field("csrf")}, "action": {"withdraw"}}); code != 409 {
		t.Fatal("unconfirmed decrease withdrawn", code)
	}
	withdrawQuote, e := getters.SaveAlertsStripeDecreaseQuote(ctx, app, people[1], b.IntentID, 1, decrease.Preview)
	if e != nil {
		t.Fatal(e)
	}
	for _, statement := range []string{
		`INSERT INTO alerts_stripe_decreases(quote_id,checkout_id,state) VALUES($1,$2,'scheduled')`,
		`INSERT INTO alerts_stripe_decrease_attachments(quote_id,lease_until,schedule_id) VALUES($1,now(),'sub_sched_' || $2::text)`,
		`INSERT INTO alerts_stripe_decrease_schedules(quote_id,schedule_id,lease_until,snapshot,verified_at) VALUES($1,'sub_sched_' || $2::text,now(),'{}',now())`,
	} {
		if _, e = pool.Exec(ctx, statement, withdrawQuote.ID, b.IntentID); e != nil {
			t.Fatal(e)
		}
	}
	withdrawPath := "/services/alerts/decrease/" + withdrawQuote.ID
	code, html, _ = call(withdrawPath, nil)
	if code != 200 || !strings.Contains(html, "Keep my current plan") || strings.Contains(html, "Confirm decrease") {
		t.Fatal("withdrawal control", code, html)
	}
	if code, _, _ := call(withdrawPath, url.Values{"csrf": {"wrong"}, "action": {"withdraw"}}); code != 403 {
		t.Fatal("withdrawal csrf", code)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO alerts_stripe_decrease_withdrawals(quote_id) VALUES($1)`, withdrawQuote.ID); e != nil {
		t.Fatal(e)
	}
	code, html, _ = call(withdrawPath, nil)
	if code != 200 || !strings.Contains(html, "request to keep your current plan is saved") || strings.Contains(html, "name=\"action\"") {
		t.Fatal("pending withdrawal", code, html)
	}
	if _, e = pool.Exec(ctx, `UPDATE alerts_stripe_decrease_withdrawals SET state='released',snapshot='{}' WHERE quote_id=$1`, withdrawQuote.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='cancelled' WHERE quote_id=$1`, withdrawQuote.ID); e != nil {
		t.Fatal(e)
	}
	code, html, _ = call(withdrawPath, nil)
	if code != 200 || !strings.Contains(html, "Continuing annual total") || !strings.Contains(html, "$48.00") || strings.Contains(html, "2 → 1") {
		t.Fatal("withdrawn plan summary", code, html)
	}
	if code, _, location := call(withdrawPath, url.Values{"csrf": {cancelForm.Get("csrf")}, "action": {"withdraw"}}); code != 303 || location != withdrawPath {
		t.Fatal("withdrawal replay", code, location)
	}
	for i := 0; i < 2; i++ {
		if code, body, location := call("/services/alerts/subscribe?account="+people[1]+"&slots=3", nil); code != 303 || location != upgradePath {
			t.Fatal("existing-plan upgrade handoff", code, body, location)
		}
	}
	if code, _, location := call("/services/alerts/subscribe?account="+people[1]+"&slots=2", nil); code != 303 || location != "/services/alerts/manage" {
		t.Fatal("already covered selection", code, location)
	}
	code, html, _ = call(upgradePath+"?paid=true&total=1", nil)
	if code != 200 || !strings.Contains(html, "$1.95") || !strings.Contains(html, "$0.15") || !strings.Contains(html, "$72.00") || !strings.Contains(html, "Confirm upgrade") || strings.Contains(html, "Payment verified") {
		t.Fatal("upgrade review", code, html)
	}
	if code, _, _ := call(upgradePath, url.Values{"csrf": {"wrong"}, "action": {"confirm"}}); code != 403 {
		t.Fatal("upgrade csrf", code)
	}
	if code, _, _ := call("/fixture/login/"+people[0], nil); code != 204 {
		t.Fatal(code)
	}
	// The CSRF token is account-session bound; either fence prevents foreign mutation.
	if code, _, _ := call("/services/alerts/manage", cancelForm); code != 403 && code != 404 {
		t.Fatal("foreign cancellation accepted", code)
	}
	if code, _, _ := call(upgradePath, nil); code != 404 {
		t.Fatal("foreign upgrade readable", code)
	}
	if code, _, _ := call(decreasePath, nil); code != 404 {
		t.Fatal("foreign decrease readable", code)
	}
	if code, _, _ := call("/fixture/login/"+people[1], nil); code != 204 {
		t.Fatal(code)
	}
	code, html, _ = call("/services/alerts/manage", nil)
	if code != 200 {
		t.Fatal(code, html)
	}
	cancelForm.Set("csrf", field("csrf"))
	for i := 0; i < 2; i++ {
		if code, body, location := call("/services/alerts/manage", cancelForm); code != 303 || location != "/services/alerts/manage" {
			t.Fatal(code, body, location)
		}
	}
	code, html, _ = call(upgradePath, nil)
	if code != 200 {
		t.Fatal(code, html)
	}
	if code, _, _ := call(upgradePath, url.Values{"csrf": {field("csrf")}, "action": {"confirm"}}); code != 409 {
		t.Fatal("stale upgrade confirmation", code)
	}
	code, html, _ = call(decreasePath, nil)
	if code != 200 {
		t.Fatal(code, html)
	}
	if code, _, _ := call(decreasePath, url.Values{"csrf": {field("csrf")}, "action": {"confirm"}}); code != 409 {
		t.Fatal("stale decrease confirmation", code)
	}
	code, html, _ = call("/services/alerts/manage", nil)
	if code != 200 || !strings.Contains(html, "scheduled to stop renewing") || strings.Contains(html, "Cancel future renewals") {
		t.Fatal("missing confirmed cancellation", code, html)
	}
	lock.Lock()
	count := cancellations
	lock.Unlock()
	if count != 1 {
		t.Fatal("repeated POST duplicated cancellation", count)
	}

}
