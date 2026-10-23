package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v86"
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real account sessions, PostgreSQL, handlers and Stripe SDK serialization;
// provider HTTP stays local and never changes a real subscription.
func TestAlertsStripeDecreaseHTTPPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated migrated database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{StripeKey: "sk_test_decrease_fixture"}}
	app.TemplateCache, err = template.ParseFiles("../../templates/alerts_stripe_manage.tmpl", "../../templates/alerts_stripe_decrease.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var person string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Decrease HTTP fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); e != nil {
			t.Error(e)
		}
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	checkout, err := getters.ReserveAlertsStripeCheckout(app, person, "decrease-http-fixture", 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	id := checkout.Intent.ID
	b := alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, CustomerID: "cus_" + id, PriceID: "price_" + id, ProductID: "prod_" + id, PaidSlots: 2}
	raw, _ := json.Marshal(b)
	if _, err = pool.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, id, b.SubscriptionID, raw); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Hour)
	end := start.AddDate(1, 0, 0)
	price := &stripe.Price{ID: b.PriceID, Currency: "usd", UnitAmount: 2400, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "year", IntervalCount: 1}, Product: &stripe.Product{ID: b.ProductID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	meta := map[string]string{"account_id": person, "checkout_intent": id, "product": "alerts.repository-slots"}
	sub := &stripe.Subscription{ID: b.SubscriptionID, Customer: &stripe.Customer{ID: b.CustomerID}, Metadata: meta, Status: "active", CollectionMethod: "charge_automatically", LatestInvoice: &stripe.Invoice{ID: "in_base_" + id}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{ID: "si_" + id, Quantity: 2, Price: price, CurrentPeriodStart: start.Unix(), CurrentPeriodEnd: end.Unix()}}}}
	base := &stripe.Invoice{ID: sub.LatestInvoice.ID, Status: "paid", Currency: "usd", Customer: sub.Customer, BillingReason: "subscription_create", Total: 4800, AmountPaid: 4800, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: start.Unix()}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: &stripe.Subscription{ID: b.SubscriptionID}, Metadata: meta}}, Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{{Currency: "usd", Quantity: 2, Amount: 4800, Period: &stripe.Period{Start: start.Unix(), End: end.Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: b.SubscriptionID}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: b.ProductID}}}}}}
	var mu sync.Mutex
	var schedule *stripe.SubscriptionSchedule
	creates, updates, releases, cancellations := 0, 0, 0, 0
	sid := "sub_sched_" + id
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			if e := r.ParseForm(); e != nil {
				t.Error(e)
			}
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/prices/"+b.PriceID:
			json.NewEncoder(w).Encode(price)
		case r.Method == "GET" && r.URL.Path == "/v1/invoices/"+base.ID:
			json.NewEncoder(w).Encode(base)
		case r.Method == "GET" && r.URL.Path == "/v1/subscriptions/"+b.SubscriptionID:
			json.NewEncoder(w).Encode(sub)
		case r.Method == "POST" && r.URL.Path == "/v1/subscription_schedules":
			creates++
			if len(r.PostForm) != 1 || r.Form.Get("from_subscription") != b.SubscriptionID || !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "alerts-decrease-create:") {
				t.Error("unsafe attachment", r.Form)
			}
			schedule = &stripe.SubscriptionSchedule{ID: sid, Status: "active", EndBehavior: "release", Customer: sub.Customer, Subscription: &stripe.Subscription{ID: b.SubscriptionID}, CurrentPhase: &stripe.SubscriptionScheduleCurrentPhase{StartDate: start.Unix(), EndDate: end.Unix()}, Phases: []*stripe.SubscriptionSchedulePhase{{StartDate: start.Unix(), EndDate: end.Unix(), Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: price, Quantity: 2}}}}}
			sub.Schedule = &stripe.SubscriptionSchedule{ID: sid}
			json.NewEncoder(w).Encode(schedule)
		case r.Method == "GET" && r.URL.Path == "/v1/subscription_schedules/"+sid:
			json.NewEncoder(w).Encode(schedule)
		case r.Method == "POST" && r.URL.Path == "/v1/subscription_schedules/"+sid:
			updates++
			quote := r.Form.Get("metadata[alerts_decrease]")
			if r.Header.Get("Idempotency-Key") != "alerts-decrease-update:"+quote || r.Form.Get("proration_behavior") != "none" || r.Form.Get("end_behavior") != "release" || r.Form.Get("phases[0][items][0][quantity]") != "2" || r.Form.Get("phases[1][items][0][quantity]") != "1" || r.Form.Get("phases[0][end_date]") != strconv.FormatInt(end.Unix(), 10) || r.Form.Get("phases[1][start_date]") != strconv.FormatInt(end.Unix(), 10) || r.Form.Get("phases[1][duration][interval]") != "year" {
				t.Error("unsafe phases", r.Form)
			}
			schedule.Metadata = map[string]string{"alerts_decrease": quote, "account_id": person, "checkout_intent": id}
			schedule.Phases = nil
			method := stripe.SubscriptionCollectionMethod("charge_automatically")
			for i, quantity := range []int64{2, 1} {
				prefix := "phases[" + strconv.Itoa(i) + "]"
				if r.Form.Get(prefix+"[automatic_tax][enabled]") != "true" || r.Form.Get(prefix+"[proration_behavior]") != "none" {
					t.Error("tax or proration", r.Form)
				}
				at, until := start.Unix(), end.Unix()
				if i == 1 {
					at = end.Unix()
					until = end.AddDate(1, 0, 0).Unix()
				}
				schedule.Phases = append(schedule.Phases, &stripe.SubscriptionSchedulePhase{StartDate: at, EndDate: until, Currency: "usd", CollectionMethod: &method, AutomaticTax: &stripe.SubscriptionAutomaticTax{Enabled: true}, ProrationBehavior: "none", Metadata: meta, Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: price, Quantity: quantity}}})
			}
			json.NewEncoder(w).Encode(schedule)
		case r.Method == "POST" && r.URL.Path == "/v1/subscription_schedules/"+sid+"/release":
			releases++
			if r.Form.Get("preserve_cancel_date") != "true" || !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "alerts-decrease-withdraw:") {
				t.Error("unsafe withdrawal", r.Form)
			}
			schedule.Status = "released"
			schedule.Subscription = nil
			schedule.ReleasedSubscription = &stripe.Subscription{ID: b.SubscriptionID}
			schedule.CurrentPhase = nil
			schedule.ReleasedAt = time.Now().Unix()
			sub.Schedule = nil
			json.NewEncoder(w).Encode(schedule)
		case r.Method == "POST" && r.URL.Path == "/v1/subscriptions/"+b.SubscriptionID:
			cancellations++
			if r.Form.Get("cancel_at_period_end") != "true" || sub.Schedule != nil {
				t.Error("unsafe cancellation", r.Form)
			}
			sub.CancelAtPeriodEnd = true
			json.NewEncoder(w).Encode(sub)
		default:
			t.Error("unexpected provider request", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	backend, key := stripe.GetBackend(stripe.APIBackend), stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	stripe.Key = app.Env.StripeKey
	defer func() { stripe.SetBackend(stripe.APIBackend, backend); stripe.Key = key }()
	if _, err = getters.ReconcileAlertsStripeInvoice(ctx, app, person, id, base.ID, alertsstripe.NewClient(false)); err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	router.HandleFunc("/fixture/login", func(w http.ResponseWriter, r *http.Request) {
		if e := auth.LoginPerson(app, r, person, auth.MethodPassword); e != nil {
			t.Error(e)
		}
		w.WriteHeader(204)
	})
	router.HandleFunc("/services/alerts/manage", func(w http.ResponseWriter, r *http.Request) { AlertsStripeManage(w, r, app) })
	router.HandleFunc("/services/alerts/decrease/{quote}", func(w http.ResponseWriter, r *http.Request) { AlertsStripeDecrease(w, r, app) })
	server := httptest.NewServer(app.Session.LoadAndSave(router))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(path string, form url.Values) (int, string, string) {
		t.Helper()
		var res *http.Response
		var e error
		if form == nil {
			res, e = browser.Get(server.URL + path)
		} else {
			res, e = browser.PostForm(server.URL+path, form)
		}
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		body, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		return res.StatusCode, string(body), res.Header.Get("Location")
	}
	field := func(body, name string) string {
		t.Helper()
		m := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(body)
		if len(m) != 2 {
			t.Fatal("missing field", name, body)
		}
		return m[1]
	}
	call("/fixture/login", nil)
	code, body, _ := call("/services/alerts/manage", nil)
	if code != 200 || !strings.Contains(body, "Review lower annual total") {
		t.Fatal(code, body)
	}
	code, body, path := call("/services/alerts/manage", url.Values{"csrf": {field(body, "csrf")}, "checkout": {id}, "action": {"decrease"}, "slots": {"1"}})
	if code != 303 || !strings.HasPrefix(path, "/services/alerts/decrease/") {
		t.Fatal(code, body, path)
	}
	code, body, _ = call(path, nil)
	if code != 200 || !strings.Contains(body, "$24.00") {
		t.Fatal(code, body)
	}
	csrf := field(body, "csrf")
	for i := 0; i < 2; i++ {
		if code, body, location := call(path, url.Values{"csrf": {csrf}, "action": {"confirm"}, "slots": {"999"}, "total": {"1"}}); code != 303 || location != path {
			t.Fatal(code, body, location)
		}
	}
	code, body, _ = call(path, nil)
	if code != 200 || !strings.Contains(body, "Your decrease is scheduled") || !strings.Contains(body, "Keep my current plan") {
		t.Fatal(code, body)
	}
	quoteID := strings.TrimPrefix(path, "/services/alerts/decrease/")
	var status string
	if err = pool.QueryRow(ctx, `SELECT state FROM alerts_stripe_decreases WHERE quote_id=$1`, quoteID).Scan(&status); err != nil || status != "scheduled" {
		t.Fatal(status, err)
	}
	code, body, _ = call("/services/alerts/manage", nil)
	if code != 200 || !strings.Contains(body, "View subscription change") || strings.Contains(body, "Cancel future renewals") {
		t.Fatal(code, body)
	}
	for i := 0; i < 2; i++ {
		if code, body, location := call(path, url.Values{"csrf": {csrf}, "action": {"withdraw"}}); code != 303 || location != path {
			t.Fatal(code, body, location)
		}
	}
	code, body, _ = call(path, nil)
	if code != 200 || !strings.Contains(body, "planned decrease was withdrawn") || !strings.Contains(body, "$48.00") {
		t.Fatal(code, body)
	}
	code, body, _ = call("/services/alerts/manage", nil)
	if code != 200 || !strings.Contains(body, "Cancel future renewals") {
		t.Fatal("cancellation not restored", code, body)
	}
	cancelForm := url.Values{}
	for _, name := range []string{"csrf", "checkout", "key", "revision", "periodEnd"} {
		cancelForm.Set(name, field(body, name))
	}
	if code, body, _ := call("/services/alerts/manage", cancelForm); code != 303 {
		t.Fatal(code, body)
	}
	grants, err := getters.ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 2 || !grants[0].EndsAt.Equal(end) {
		t.Fatal("paid access changed", grants, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 || updates != 1 || releases != 1 || cancellations != 1 || sub.Items.Data[0].Quantity != 2 || !sub.CancelAtPeriodEnd {
		t.Fatal("duplicate or incorrect provider changes", creates, updates, releases, cancellations, sub)
	}
}
