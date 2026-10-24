package handlers

import (
	"btcpp-web/internal/emails"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/expedition"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v86"
)

func TestAntarcticaCheckoutClosed(t *testing.T) {
	app := antarcticaTestTemplates(t)
	w := httptest.NewRecorder()
	AntarcticaCheckout(w, httptest.NewRequest("POST", "/expeditions/antarctica/checkout", nil), app)
	if w.Code != 503 {
		t.Fatalf("closed checkout: %d", w.Code)
	}
}

// Explicit disposable database only. Provider calls go to a local fake Stripe
// API; no test or live Stripe credentials are used.
func TestAntarcticaCheckoutHTTPPostgres(t *testing.T) {
	for _, scenario := range []struct {
		provider, plan string
		amount         int64
	}{
		{"stripe", "full", 1670000}, {"stripe", "installments", 334000}, {"opennode", "full", 1586500}, {"opennode", "installments", 334000}, {"opennode", "full", 1}, {"stripe", "full", 50},
	} {
		t.Run(scenario.provider+"_"+scenario.plan+fmt.Sprint(scenario.amount), func(t *testing.T) {
			testAntarcticaCheckoutHTTP(t, scenario.provider, scenario.plan, scenario.amount)
		})
	}
}
func testAntarcticaCheckoutHTTP(t *testing.T, paymentProvider, plan string, amount int64) {
	uri := os.Getenv("EXPEDITION_COMMERCE_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("requires isolated migrated database")
	}
	app := antarcticaTestTemplates(t)
	beta := amount == 1 || amount == 50
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app.DB = pool
	app.Session = scs.New()
	defer enableAntarcticaSalesForTest(t, app)()
	app.Env.OpenNode.Key = "local-fixture"
	if paymentProvider == "stripe" {
		app.Env.StripeKey = "sk_test_fixture"
	}
	var buyer string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Expedition HTTP fixture') RETURNING id::text`).Scan(&buyer); err != nil {
		t.Fatal(err)
	}
	email := buyer + "@example.test"
	if beta {
		email = "nifty@btcpp.dev"
	}
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, buyer, email); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO expedition_inventory(berth,capacity,sales_open,booking_closes_at,hold_seconds) VALUES('shared',18,true,'2028-01-01',3600) ON CONFLICT(berth) DO UPDATE SET capacity=18,sales_open=true,hold_seconds=3600`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if beta {
			pool.Exec(ctx, `DELETE FROM expedition_beta_claims WHERE buyer_id=$1`, buyer)
			pool.Exec(ctx, `DELETE FROM person_emails WHERE person_id=$1`, buyer)
		}
		pool.Exec(ctx, `DELETE FROM expedition_allocations WHERE reservation_id IN(SELECT id FROM expedition_reservations WHERE buyer_id=$1)`, buyer)
		pool.Exec(ctx, `DELETE FROM expedition_installments WHERE reservation_id IN(SELECT id FROM expedition_reservations WHERE buyer_id=$1)`, buyer)
		pool.Exec(ctx, `DELETE FROM expedition_reservations WHERE buyer_id=$1`, buyer)
	}()
	policy := getters.ServiceLegalPolicy{ProductCode: expedition.ProductCode, Version: uuid.NewString(), TermsText: expedition.BookingTerms, PrivacyText: "Test privacy only", EffectiveAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err = getters.RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err = getters.ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal(err)
	}
	if !beta {
		if _, err := (&expedition.Store{DB: pool}).ReserveBetaParty(ctx, buyer, uuid.NewString(), []expedition.CabinSelection{{Berth: "shared", Quantity: 1, Preference: "coed"}}); err != expedition.ErrInvalid {
			t.Fatal("non-allowlisted identity received beta price", err)
		}
	} else {
		if _, err := pool.Exec(ctx, `DELETE FROM person_emails WHERE person_id=$1`, buyer); err != nil {
			t.Fatal(err)
		}
		if _, err := (&expedition.Store{DB: pool}).ReserveBetaParty(ctx, buyer, uuid.NewString(), []expedition.CabinSelection{{Berth: "shared", Quantity: 1, Preference: "coed"}}); err != expedition.ErrInvalid {
			t.Fatal("unverified identity received beta price", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,'nifty@btcpp.dev',true,now())`, buyer); err != nil {
			t.Fatal(err)
		}
	}
	sessionID := "cs_test_" + strings.ReplaceAll(buyer, "-", "")
	var sessionResponse map[string]any
	taxCalls, creates := 0, 0
	chargeID := uuid.NewString()
	var attemptID atomic.Value
	var paid atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		r.ParseForm()
		switch {
		case r.URL.Path == "/v1/charges" && r.Method == "POST":
			creates++
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload["amount"] != fmt.Sprintf("%d.%02d", amount/100, amount%100) || payload["currency"] != "USD" || payload["customer_email"] != email || r.Header.Get("Authorization") != "local-fixture" {
				t.Error("incorrect Bitcoin charge", payload)
			}
			attemptID.Store(payload["order_id"])
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": chargeID, "order_id": attemptID.Load()}})
		case r.URL.Path == "/v2/charge/"+chargeID:
			status := "unpaid"
			if paid.Load() {
				status = "paid"
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": chargeID, "order_id": attemptID.Load(), "status": status, "currency": "USD", "source_fiat_value": float64(amount) / 100, "price": amount, "fee": 0, "missing_amt": 0, "hosted_checkout_url": "https://checkout.opennode.com/" + chargeID}})
		case strings.HasPrefix(r.URL.Path, "/v1/tax/"):
			taxCalls++
			t.Errorf("conference checkout must not call Stripe Tax: %s", r.URL.Path)
			http.Error(w, "unexpected tax request", 500)
		case r.URL.Path == "/v1/checkout/sessions" && r.Method == "POST":
			creates++
			if r.Header.Get("Idempotency-Key") == "" || r.Form.Get("payment_method_types[0]") != "card" || r.Form.Get("line_items[0][price_data][unit_amount]") != strconv.FormatInt(amount, 10) {
				t.Error("wrong payment request", r.Form)
			}
			expires, _ := strconv.ParseInt(r.Form.Get("expires_at"), 10, 64)
			sessionResponse = map[string]any{"id": sessionID, "url": "https://checkout.stripe.com/c/pay/" + sessionID, "mode": "payment", "currency": "usd", "amount_total": amount, "status": "open", "expires_at": expires, "metadata": map[string]string{"checkout-kind": "service", "service-attempt-id": r.Form.Get("metadata[service-attempt-id]"), "service-order-id": r.Form.Get("metadata[service-order-id]")}}
			json.NewEncoder(w).Encode(sessionResponse)
		case r.URL.Path == "/v1/checkout/sessions/"+sessionID:
			json.NewEncoder(w).Encode(sessionResponse)
		default:
			t.Errorf("unexpected provider request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	app.Env.OpenNode.Endpoint = provider.URL + "/v1"
	previous := stripe.GetBackend(stripe.APIBackend)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	defer stripe.SetBackend(stripe.APIBackend, previous)
	router := mux.NewRouter()
	router.HandleFunc("/login-fixture", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, buyer, auth.MethodPassword); err != nil {
			t.Error(err)
		}
		w.WriteHeader(204)
	})
	router.HandleFunc("/expeditions/antarctica/checkout", func(w http.ResponseWriter, r *http.Request) { AntarcticaCheckout(w, r, app) })
	router.HandleFunc("/services/orders/{order}", func(w http.ResponseWriter, r *http.Request) { ServiceOrder(w, r, app) })
	router.HandleFunc("/services/orders/{order}/pay", func(w http.ResponseWriter, r *http.Request) { ServiceOrderPay(w, r, app) })
	router.HandleFunc("/callback/services/opennode", func(w http.ResponseWriter, r *http.Request) { ServiceOpenNodeCallback(w, r, app) })
	router.HandleFunc("/expeditions/bookings/{booking}", func(w http.ResponseWriter, r *http.Request) { ExpeditionBooking(w, r, app) })
	router.HandleFunc("/expeditions/bookings/{booking}/installments/{number}", func(w http.ResponseWriter, r *http.Request) { ExpeditionInstallment(w, r, app) })
	server := httptest.NewServer(app.Session.LoadAndSave(router))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) (int, string) {
		t.Helper()
		res, e := client.Get(server.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	code, _ := get("/expeditions/antarctica/checkout")
	if code != 303 {
		t.Fatal("anonymous checkout", code)
	}
	get("/login-fixture")

	code, html := get("/expeditions/antarctica/checkout")
	if code != 200 {
		t.Fatal(code, html)
	}
	if strings.Contains(html, "Save 5% when paying in full") {
		t.Fatal("Bitcoin option still advertises the discount")
	}
	if strings.Contains(html, `name="provider" value="stripe"`) != (paymentProvider == "stripe") {
		t.Fatal("Stripe option does not match configured availability")
	}

	attr := func(name string) string {
		t.Helper()
		m := regexp.MustCompile(name + `="([^"]+)"`).FindStringSubmatch(html)
		if len(m) != 2 {
			t.Fatal("missing", name, html)
		}
		return m[1]
	}
	request := antarcticaCheckoutRequest{CSRF: attr("data-csrf"), Key: attr("data-request-key"), Spots: 1, Selections: []expedition.CabinSelection{{Berth: "shared", Quantity: 1, Preference: "female"}}, Plan: plan, Provider: paymentProvider, TermsVersion: policy.Version, Accepted: true, ExpectedTotal: amount, ExpectedDue: amount}
	if plan == "installments" {
		request.ExpectedTotal = 1670000
	}
	post := func(q antarcticaCheckoutRequest) (int, string) {
		t.Helper()
		b, _ := json.Marshal(q)
		res, e := client.Post(server.URL+"/expeditions/antarctica/checkout", "application/json", bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(out)
	}
	bad := request
	bad.CSRF = "wrong"
	if code, _ = post(bad); code != 403 {
		t.Fatal("CSRF", code)
	}
	bad = request
	bad.Accepted = false
	if code, _ = post(bad); code != 422 {
		t.Fatal("terms", code)
	}
	bad = request
	bad.TermsVersion = "old"
	if code, _ = post(bad); code != 422 {
		t.Fatal("stale terms", code)
	}
	bad = request
	bad.ExpectedDue++
	if code, _ = post(bad); code != 422 {
		t.Fatal("stale signup amount", code)
	}
	bad = request
	bad.ExpectedTotal = amount + 1
	if code, _ = post(bad); code != 422 {
		t.Fatal("client price", code)
	}
	if beta {
		if !strings.Contains(html, `data-beta="true"`) || !strings.Contains(html, `data-price="1"`) {
			t.Fatal("missing beta display")
		}
		bad = request
		bad.Spots = 2
		bad.Selections = []expedition.CabinSelection{{Berth: "shared", Quantity: 2, Preference: "coed"}}
		if code, _ = post(bad); code != 422 {
			t.Fatal("beta allowed multiple berths", code)
		}
		bad = request
		bad.Plan = "installments"
		if code, _ = post(bad); code != 422 {
			t.Fatal("beta allowed installments", code)
		}
	}
	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM expedition_reservations WHERE buyer_id=$1`, buyer).Scan(&count)
	if count != 0 {
		t.Fatal("invalid request held inventory")
	}
	code, body := post(request)
	if code != 200 {
		t.Fatal(code, body)
	}
	var result map[string]string
	json.Unmarshal([]byte(body), &result)
	code, retry := post(request)
	if code != 200 || retry != body || taxCalls != 0 {
		t.Fatal("order retry", code, retry, taxCalls)
	}
	if beta {
		bad = request
		bad.Key = uuid.NewString()
		if code, _ = post(bad); code != 409 {
			t.Fatal("second beta booking", code)
		}
	}
	code, _ = get("/expeditions/antarctica/checkout")
	if code != 303 {
		t.Fatal("reload must resume order", code)
	}
	code, html = get(result["url"])
	if code != 200 || !strings.Contains(html, map[string]string{"stripe": "by card", "opennode": "with Bitcoin"}[paymentProvider]) {
		t.Fatal("order page", code, html)
	}
	csrf := attr("name=\"csrf\" value")
	for i := 0; i < 2; i++ {
		res, e := client.PostForm(server.URL+result["url"]+"/pay", url.Values{"csrf": {csrf}})
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 303 || res.Header.Get("Location") != map[string]string{"stripe": "https://checkout.stripe.com/c/pay/" + sessionID, "opennode": "https://checkout.opennode.com/" + chargeID}[paymentProvider] {
			t.Fatal("provider redirect", res.StatusCode, string(b))
		}
	}
	if creates != 1 {
		t.Fatal("duplicate provider sessions", creates)
	}
	pool.QueryRow(ctx, `SELECT count(*) FROM expedition_reservations WHERE buyer_id=$1 AND state='confirmed'`, buyer).Scan(&count)
	if count != 0 {
		t.Fatal("redirect confirmed unpaid booking")
	}
	if paymentProvider == "stripe" {
		if beta {
			metadata := sessionResponse["metadata"].(map[string]string)
			if err := getters.RecordServicePayment(app, getters.VerifiedServicePayment{AttemptID: metadata["service-attempt-id"], Provider: "stripe", Reference: sessionID, EventID: "fixture-" + sessionID, Currency: "USD", TotalCents: amount, PaidAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if err := getters.FulfillExpeditionPurchase(app, metadata["service-order-id"]); err != nil {
				t.Fatal(err)
			}
			var bookingID string
			if err := pool.QueryRow(ctx, `SELECT id::text FROM expedition_reservations WHERE buyer_id=$1 AND state='confirmed'`, buyer).Scan(&bookingID); err != nil {
				t.Fatal(err)
			}
			preview, err := getters.ExpeditionCancellationPreview(ctx, app, bookingID)
			if err != nil {
				t.Fatal(err)
			}
			if err = getters.CancelExpeditionBooking(ctx, app, bookingID, buyer, preview.Token, "Repeat checkout test"); err != nil {
				t.Fatal(err)
			}
			oldKey := request.Key
			code, html = get("/expeditions/antarctica/checkout")
			if code != 200 {
				t.Fatal("cancelled checkout did not restart", code, html)
			}
			request.Key = attr("data-request-key")
			request.CSRF = attr("data-csrf")
			if request.Key == oldKey {
				t.Fatal("cancelled checkout reused request key")
			}
			code, second := post(request)
			if code != 200 || second == body {
				t.Fatal("second beta booking failed", code, second)
			}
			var oldState, active string
			if err = pool.QueryRow(ctx, `SELECT state FROM expedition_reservations WHERE id=$1`, bookingID).Scan(&oldState); err != nil || oldState != "cancelled" {
				t.Fatal("old booking changed", oldState, err)
			}
			if err = pool.QueryRow(ctx, `SELECT reservation_id::text FROM expedition_beta_claims WHERE buyer_id=$1`, buyer).Scan(&active); err != nil || active == bookingID {
				t.Fatal("active beta claim not moved", active, err)
			}
			request.Key = uuid.NewString()
			if code, _ := post(request); code != 409 {
				t.Fatal("multiple active test bookings allowed", code)
			}
		}
		if plan == "installments" {
			metadata := sessionResponse["metadata"].(map[string]string)
			orderID := metadata["service-order-id"]
			payment := getters.VerifiedServicePayment{AttemptID: metadata["service-attempt-id"], Provider: "stripe", Reference: sessionID, EventID: "fixture-" + sessionID, Currency: "USD", TotalCents: amount, PaidAt: time.Now().UTC()}
			if err := getters.RecordServicePayment(app, payment); err != nil {
				t.Fatal(err)
			}
			if err := getters.FulfillExpeditionPurchase(app, orderID); err != nil {
				t.Fatal(err)
			}
			var bookingID string
			if err := pool.QueryRow(ctx, `SELECT id::text FROM expedition_reservations WHERE buyer_id=$1 AND state='confirmed'`, buyer).Scan(&bookingID); err != nil {
				t.Fatal(err)
			}
			// Move this test fixture into the second installment's payment window.
			if _, err := pool.Exec(ctx, `UPDATE expedition_reservations SET schedule=jsonb_set(schedule,'{installments,1,due_at}',to_jsonb(now()+interval '1 day')) WHERE id=$1`, bookingID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE expedition_installments SET due_at=now()+interval '1 day' WHERE reservation_id=$1 AND number=2`, bookingID); err != nil {
				t.Fatal(err)
			}
			path := "/expeditions/bookings/" + bookingID + "/installments/2"
			code, page := get(path)
			if code != 200 || !strings.Contains(page, "Continue to Stripe payment") || strings.Contains(page, "Billing address") {
				t.Fatal("Stripe installment page", code, page)
			}
			field := func(name string) string {
				m := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(page)
				if len(m) != 2 {
					t.Fatal("missing field", name)
				}
				return m[1]
			}
			res, err := client.PostForm(server.URL+path, url.Values{"csrf": {field("csrf")}, "key": {field("key")}, "version": {field("version")}, "accepted": {"yes"}})
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != 303 {
				b, _ := io.ReadAll(res.Body)
				t.Fatal("Stripe installment checkout", res.StatusCode, string(b))
			}
			nextOrder := strings.TrimPrefix(res.Header.Get("Location"), "/services/orders/")
			var provider string
			var total int64
			if err := pool.QueryRow(ctx, `SELECT payment_provider,total_cents FROM shop_orders WHERE public_id=$1`, nextOrder).Scan(&provider, &total); err != nil || provider != "stripe" || total != 334000 {
				t.Fatal("incorrect installment order", provider, total, err)
			}
		}
		return
	}
	callback := func(signature string) int {
		res, err := client.PostForm(server.URL+"/callback/services/opennode", url.Values{"id": {chargeID}, "hashed_order": {signature}, "status": {"paid"}, "price": {"1"}})
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		return res.StatusCode
	}
	if code := callback("invalid"); code < 400 {
		t.Fatal("unsigned callback accepted", code)
	}
	signature := computeHash(app.Env.OpenNode.Key, chargeID)
	if code := callback(signature); code != 204 {
		t.Fatal("unpaid callback", code)
	}
	pool.QueryRow(ctx, `SELECT count(*) FROM expedition_reservations WHERE buyer_id=$1 AND state='confirmed'`, buyer).Scan(&count)
	if count != 0 {
		t.Fatal("callback body overrode provider state")
	}
	paid.Store(true)
	for i := 0; i < 2; i++ {
		if code := callback(signature); code != 204 {
			t.Fatal("paid callback", code)
		}
	}
	if worked, err := processServiceFulfillment(ctx, app, attemptID.Load().(string), time.Now().Add(time.Second)); err != nil || !worked {
		t.Fatal("fulfillment worker", worked, err)
	}
	var bookingID, orderID string
	if err = pool.QueryRow(ctx, `SELECT r.id::text,i.paid_order_id::text FROM expedition_reservations r JOIN expedition_installments i ON i.reservation_id=r.id AND i.number=1 WHERE r.buyer_id=$1 AND r.state='confirmed'`, buyer).Scan(&bookingID, &orderID); err != nil {
		t.Fatal("booking not confirmed", err)
	}
	b, err := getters.ExpeditionBookingForPerson(ctx, app, buyer, bookingID)
	wantPayments := 1
	if plan == "installments" {
		wantPayments = 5
	}
	if err != nil || len(b.Payments) != wantPayments || !b.Payments[0].Applied || b.Payments[0].Cents != amount {
		t.Fatal("booking schedule", b, err)
	}
	owned, err := getters.AccountExpeditionBookings(ctx, app, buyer)
	if err != nil || len(owned) != 1 || owned[0].ID != bookingID {
		t.Fatal("booking summary missing or duplicated", owned, err)
	}
	unrelated, err := getters.AccountExpeditionBookings(ctx, app, uuid.NewString())
	if err != nil || len(unrelated) != 0 {
		t.Fatal("booking summary exposed another account", unrelated, err)
	}
	if code, body := get("/expeditions/bookings/" + bookingID); code != 200 || !strings.Contains(body, "January 24") {
		t.Fatal("booking page", code, body)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM service_payment_events e JOIN service_payment_attempts a ON a.id=e.attempt_id JOIN service_purchase_requests p ON p.id=a.purchase_id WHERE p.order_id=$1`, orderID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate settlement", count, err)
	}
	var taxProvider, destination string
	var taxAmount int
	if err := pool.QueryRow(ctx, `SELECT sales_tax_provider,sales_tax_amount_cents,destination_country FROM tax_quotes WHERE order_id=$1`, orderID).Scan(&taxProvider, &taxAmount, &destination); err != nil || taxProvider != "conference_ticket" || taxAmount != 0 || destination != "" {
		t.Fatalf("address-free tax policy evidence: %s %d %s %v", taxProvider, taxAmount, destination, err)
	}
	if code, body := get(result["url"]); code != 200 || !strings.Contains(body, "/invoice") {
		t.Fatal("paid order missing optional invoice link", code)
	}
	app.Env.MailEndpoint = "http://never-contacted.invalid"
	app.Env.LocalExternal = "https://btcpp.dev"
	sent := 0
	if worked, err := processServiceReceiptWithSender(ctx, app, orderID, time.Now().Add(time.Second), func(m *emails.Mail) error {
		sent++
		if m.Email != email || !strings.Contains(string(m.TextBody), "You’re going to Antarctica") || len(m.Files) != 1 || !strings.Contains(string(m.Files[0].Bytes), "DTSTART;VALUE=DATE:20280124") {
			t.Fatal("welcome contents or calendar", m.Email)
		}
		return nil
	}); err != nil || !worked || sent != 1 {
		t.Fatal("welcome delivery", worked, sent, err)
	}
}

func TestAntarcticaCheckoutAccountHandoff(t *testing.T) {
	const next = "/expeditions/antarctica/checkout"
	for _, tc := range []struct {
		identity *auth.Identity
		path     string
	}{
		{nil, "/login"},
		{&auth.Identity{}, "/login"},
		{&auth.Identity{LoginEmail: "   "}, "/login"},
		{&auth.Identity{LoginEmail: "new@example.test"}, "/dashboard/profile"},
	} {
		dest, err := url.Parse(antarcticaCheckoutAccountURL(tc.identity))
		if err != nil || dest.Path != tc.path || dest.Query().Get("next") != next {
			t.Fatalf("bad handoff: %v %v", dest, err)
		}
	}
	// Both validation errors and successful profile creation retain checkout.
	retry, _ := url.Parse(dashboardProfileURLWithFlash("", "", next, "Name is required."))
	if retry.Path != "/dashboard/profile" || retry.Query().Get("next") != next {
		t.Fatal("lost checkout after profile validation")
	}
	success, _ := url.Parse(dashboardProfileSuccessRedirect("", "", next, "Profile created."))
	if success.Path != next {
		t.Fatal("lost checkout after profile creation")
	}
}
