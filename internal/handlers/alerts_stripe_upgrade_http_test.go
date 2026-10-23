package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
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

func TestAlertsStripeUpgradeHTTPPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated migrated database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{Host: "localhost", Port: "8898", StripeKey: "sk_test_upgrade_fixture"}}
	app.TemplateCache, err = template.ParseFiles("../../templates/alerts_stripe_upgrade.tmpl", "../../templates/alerts_stripe_start.tmpl", "../../templates/service_order.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var person string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Upgrade HTTP fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	checkout, err := getters.ReserveAlertsStripeCheckout(app, person, "upgrade-http-fixture", 2, "https://btcpp.dev")
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
	line := func(quantity, amount, at int64, proration bool) *stripe.InvoiceLineItem {
		return &stripe.InvoiceLineItem{Currency: "usd", Quantity: quantity, Amount: amount, Period: &stripe.Period{Start: at, End: end.Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: b.SubscriptionID, SubscriptionItem: "si_" + id, Proration: proration}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: b.ProductID}}}
	}
	base := &stripe.Invoice{ID: sub.LatestInvoice.ID, Status: "paid", Currency: "usd", Customer: sub.Customer, BillingReason: "subscription_create", Total: 4800, AmountPaid: 4800, AmountDue: 4800, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: start.Unix()}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: &stripe.Subscription{ID: b.SubscriptionID}, Metadata: meta}}, Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{line(2, 4800, start.Unix(), false)}}}
	var lock sync.Mutex
	var upgrade *stripe.Invoice
	var reviewed int64
	desired := int64(3)
	upgradeInvoices := map[string]*stripe.Invoice{}
	updates := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/prices/"+b.PriceID:
			json.NewEncoder(w).Encode(price)
		case r.Method == "GET" && r.URL.Path == "/v1/invoices/"+base.ID:
			json.NewEncoder(w).Encode(base)
		case r.Method == "GET" && upgradeInvoices[strings.TrimPrefix(r.URL.Path, "/v1/invoices/")] != nil:
			json.NewEncoder(w).Encode(upgradeInvoices[strings.TrimPrefix(r.URL.Path, "/v1/invoices/")])
		case r.Method == "GET" && r.URL.Path == "/v1/invoices":
			invoices := []*stripe.Invoice{base}
			if upgrade != nil && upgrade.Status == "paid" {
				invoices = append(invoices, upgrade)
			}
			json.NewEncoder(w).Encode(stripe.InvoiceList{Data: invoices})
		case r.Method == "POST" && r.URL.Path == "/v1/invoices/create_preview":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			at, e := strconv.ParseInt(r.Form.Get("subscription_details[proration_date]"), 10, 64)
			if e != nil {
				t.Error(e)
			}
			if reviewed != 0 && reviewed != at {
				t.Error("proration changed after review")
			}
			reviewed = at
			if r.Form.Get("subscription_details[items][0][quantity]") != strconv.FormatInt(desired, 10) {
				t.Error("wrong desired quantity", r.Form)
			}
			preview := *base
			preview.ID = "upcoming_in_upgrade"
			preview.Status = "draft"
			preview.BillingReason = "subscription_update"
			preview.Total = 195
			preview.AmountDue = 195
			preview.AmountPaid = 0
			preview.StatusTransitions = nil
			preview.TotalTaxes = []*stripe.InvoiceTotalTax{{Amount: 15, TaxBehavior: "inclusive"}}
			preview.Lines = &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{line(desired-1, -(desired-1)*195, at, true), line(desired, desired*195, at, true)}}
			json.NewEncoder(w).Encode(&preview)
		case r.URL.Path == "/v1/subscriptions/"+b.SubscriptionID:
			if r.Method == "POST" {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("payment_behavior") != "pending_if_incomplete" || r.Form.Get("proration_behavior") != "always_invoice" || r.Form.Get("items[0][quantity]") != strconv.FormatInt(desired, 10) || r.Form.Get("proration_date") != strconv.FormatInt(reviewed, 10) || r.Header.Get("Idempotency-Key") == "" {
					t.Error("unsafe upgrade request", r.Form)
				}
				updates++
				copy := *base
				upgrade = &copy
				upgrade.ID = "in_upgrade_" + strconv.Itoa(updates) + "_" + id
				upgradeInvoices[upgrade.ID] = upgrade
				upgrade.Status = "open"
				upgrade.BillingReason = "subscription_update"
				upgrade.AmountPaid = 0
				upgrade.Total = 195
				upgrade.AmountDue = 195
				upgrade.AmountRemaining = 195
				upgrade.StatusTransitions = nil
				upgrade.TotalTaxes = []*stripe.InvoiceTotalTax{{Amount: 15, TaxBehavior: "inclusive"}}
				upgrade.Lines = &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{line(desired-1, -(desired-1)*195, reviewed, true), line(desired, desired*195, reviewed, true)}}
				upgrade.HostedInvoiceURL = "https://invoice.stripe.com/i/acct_fixture/upgrade"
				sub.LatestInvoice = &stripe.Invoice{ID: upgrade.ID}
				sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{ExpiresAt: now.Add(time.Hour).Unix(), SubscriptionItems: []*stripe.SubscriptionItem{{ID: "si_" + id, Quantity: desired, Price: price}}}
			}
			json.NewEncoder(w).Encode(sub)
		default:
			t.Error("unexpected provider request", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	previous, key := stripe.GetBackend(stripe.APIBackend), stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	stripe.Key = app.Env.StripeKey
	defer func() { stripe.SetBackend(stripe.APIBackend, previous); stripe.Key = key }()
	if _, err = getters.ReconcileAlertsStripeInvoice(ctx, app, person, id, base.ID, alertsstripe.NewClient(false)); err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	router.HandleFunc("/login-fixture", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, person, auth.MethodPassword); err != nil {
			t.Error(err)
		}
		w.WriteHeader(204)
	})
	router.HandleFunc("/services/orders/{order}", func(w http.ResponseWriter, r *http.Request) { ServiceOrder(w, r, app) })
	router.HandleFunc("/services/alerts/subscribe", func(w http.ResponseWriter, r *http.Request) { AlertsStripeStart(w, r, app) })
	router.HandleFunc("/services/alerts/upgrade/{quote}", func(w http.ResponseWriter, r *http.Request) { AlertsStripeUpgrade(w, r, app) })
	server := httptest.NewServer(app.Session.LoadAndSave(router))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(path string, form url.Values) (int, string, string) {
		t.Helper()
		var res *http.Response
		var e error
		if form == nil {
			res, e = client.Get(server.URL + path)
		} else {
			res, e = client.PostForm(server.URL+path, form)
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
	policy := getters.ServiceLegalPolicy{ProductCode: servicebilling.AlertsProduct, Version: "upgrade-" + person, TermsText: "Upgrade <terms>", PrivacyText: "Upgrade privacy", EffectiveAt: now.Add(-time.Hour)}
	if err := getters.RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err := getters.ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal(err)
	}
	defer app.DB.Exec(ctx, `DELETE FROM service_legal_current WHERE product_code=$1`, policy.ProductCode)
	call("/login-fixture", nil)
	code, body, path := call("/services/alerts/subscribe?account="+person+"&slots=3", nil)
	if code != 303 || !strings.HasPrefix(path, "/services/alerts/upgrade/") {
		t.Fatal("prepare upgrade", code, body, path)
	}
	code, body, _ = call(path, nil)
	if code != 200 || !strings.Contains(body, "$1.95") || !strings.Contains(body, "$72.00") {
		t.Fatal("review", code, body)
	}
	if !strings.Contains(body, "Upgrade &lt;terms&gt;") || !strings.Contains(body, `name="accept_terms" value="yes" required`) {
		t.Fatal("missing escaped policy agreement", body)
	}
	token := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(token) != 2 {
		t.Fatal("missing csrf")
	}
	form := url.Values{"csrf": {token[1]}, "action": {"confirm"}, "total": {"1"}, "slots": {"999"}}
	for _, version := range []string{"", "stale"} {
		form.Set("legal_version", version)
		if version != "" {
			form.Set("accept_terms", "yes")
		}
		if code, body, _ := call(path, form); code != 409 {
			t.Fatal("unaccepted upgrade", code, body)
		}
	}
	lock.Lock()
	if updates != 0 {
		t.Error("unaccepted upgrade reached Stripe", updates)
	}
	lock.Unlock()
	form.Set("legal_version", policy.Version)
	form.Set("accept_terms", "yes")
	for i := 0; i < 2; i++ {
		if code, body, location := call(path, form); code != 303 || location != path {
			t.Fatal("confirm", code, body, location)
		}
	}
	code, body, _ = call(path+"?paid=true", nil)
	if code != 200 || !strings.Contains(body, "Complete payment with Stripe") || strings.Contains(body, "Payment verified") {
		t.Fatal("pending", code, body)
	}
	grants, err := getters.ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 2 {
		t.Fatal("unpaid upgrade granted", grants, err)
	}
	if code, body, location := call(path, url.Values{"csrf": {token[1]}, "action": {"pay"}}); code != 303 || location != "https://invoice.stripe.com/i/acct_fixture/upgrade" {
		t.Fatal("payment handoff", code, body, location)
	}
	lock.Lock()
	if updates != 1 {
		t.Error("duplicate update", updates)
	}
	upgrade.Status = "paid"
	upgrade.AmountPaid = 195
	upgrade.AmountRemaining = 0
	upgrade.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: time.Now().Unix()}
	sub.PendingUpdate = nil
	sub.Items.Data[0].Quantity = 3
	lock.Unlock()
	for i := 0; i < 2; i++ {
		code, body, _ = call(path, nil)
		if code != 200 || !strings.Contains(body, "Payment verified") {
			t.Fatal("paid recovery", code, body)
		}
	}
	grants, err = getters.ActiveServiceEntitlements(app, person, time.Now())
	var total int64
	for _, g := range grants {
		total += g.Quantity
	}
	if err != nil || len(grants) != 2 || total != 3 {
		t.Fatal("fulfilled allowance", grants, err)
	}
	var receiptID string
	if err := app.DB.QueryRow(ctx, `SELECT o.public_id FROM alerts_stripe_increase_payments p JOIN shop_orders o ON o.id=p.order_id WHERE p.quote_id=$1`, strings.TrimPrefix(path, "/services/alerts/upgrade/")).Scan(&receiptID); err != nil {
		t.Fatal(err)
	}
	code, body, _ = call("/services/orders/"+receiptID, nil)
	if code != 200 || !strings.Contains(body, "Accepted policy version") || !strings.Contains(body, policy.Version) || !strings.Contains(body, "Upgrade &lt;terms&gt;") || !strings.Contains(body, "Print or save receipt") {
		t.Fatal("paid upgrade receipt agreement", code, body)
	}
	// A second upgrade must verify the first proration, not interpret it as a
	// full-month invoice or require waiting until the next renewal.
	lock.Lock()
	desired = 4
	reviewed = 0
	lock.Unlock()
	code, body, secondPath := call("/services/alerts/subscribe?account="+person+"&slots=4", nil)
	if code != 303 || !strings.HasPrefix(secondPath, "/services/alerts/upgrade/") || secondPath == path {
		t.Fatal("second upgrade prepare", code, body, secondPath)
	}
	code, body, _ = call(secondPath, nil)
	if code != 200 || !strings.Contains(body, "$96.00") {
		t.Fatal("second review", code, body)
	}
	token = regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(token) != 2 {
		t.Fatal("second csrf")
	}
	if code, body, location := call(secondPath, url.Values{"csrf": {token[1]}, "action": {"confirm"}, "accept_terms": {"yes"}, "legal_version": {policy.Version}}); code != 303 || location != secondPath {
		t.Fatal("second confirmation", code, body, location)
	}
	lock.Lock()
	if updates != 2 {
		t.Error("second update count", updates)
	}
	upgrade.Status = "paid"
	upgrade.AmountPaid = 195
	upgrade.AmountRemaining = 0
	upgrade.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: time.Now().Unix()}
	sub.PendingUpdate = nil
	sub.Items.Data[0].Quantity = 4
	lock.Unlock()
	code, body, _ = call(secondPath, nil)
	if code != 200 || !strings.Contains(body, "Payment verified") {
		t.Fatal("second fulfillment", code, body)
	}
	grants, err = getters.ActiveServiceEntitlements(app, person, time.Now())
	total = 0
	for _, g := range grants {
		total += g.Quantity
		if !g.EndsAt.Equal(end) {
			t.Fatal("upgrade extended expiry", g)
		}
	}
	if err != nil || len(grants) != 3 || total != 4 {
		t.Fatal("second allowance", grants, err)
	}
	// Simulate a crash after consent was committed but before HTTP submission.
	// The background worker must finish without another customer page visit.
	lock.Lock()
	desired = 5
	reviewed = 0
	lock.Unlock()
	code, body, thirdPath := call("/services/alerts/subscribe?account="+person+"&slots=5", nil)
	if code != 303 {
		t.Fatal("background quote", code, body)
	}
	thirdID := strings.TrimPrefix(thirdPath, "/services/alerts/upgrade/")
	if _, err = getters.ReserveAlertsStripeIncreaseWithPolicy(ctx, app, person, thirdID, policy.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$2,next_reconcile_at=now() WHERE id=$1`, id, "cs_fixture_"+id); err != nil {
		t.Fatal(err)
	}
	if _, err = getters.ReconcileDueAlertsStripeCheckouts(ctx, app, alertsstripe.NewClient(false)); err != nil {
		t.Fatal("background submission", err)
	}
	lock.Lock()
	if updates != 3 {
		t.Error("background mutation count", updates)
	}
	lock.Unlock()
	var leased bool
	if err = pool.QueryRow(ctx, `SELECT next_reconcile_at>now()+interval '1 minute' AND next_reconcile_at<now()+interval '3 minutes' FROM alerts_stripe_checkouts WHERE id=$1`, id).Scan(&leased); err != nil || !leased {
		t.Fatal("upgrade retry lease", leased, err)
	}
	if _, err = getters.ReconcileDueAlertsStripeCheckouts(ctx, app, alertsstripe.NewClient(false)); err != nil {
		t.Fatal(err)
	}
	lock.Lock()
	if updates != 3 {
		t.Error("worker retried before lease", updates)
	}
	upgrade.Status = "paid"
	upgrade.AmountPaid = 195
	upgrade.AmountRemaining = 0
	upgrade.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: time.Now().Unix()}
	sub.PendingUpdate = nil
	sub.Items.Data[0].Quantity = 5
	lock.Unlock()
	if _, err = pool.Exec(ctx, `UPDATE alerts_stripe_checkouts SET next_reconcile_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = getters.ReconcileDueAlertsStripeCheckouts(ctx, app, alertsstripe.NewClient(false)); err != nil {
		t.Fatal("background payment fulfillment", err)
	}
	grants, err = getters.ActiveServiceEntitlements(app, person, time.Now())
	total = 0
	for _, g := range grants {
		total += g.Quantity
	}
	if err != nil || len(grants) != 4 || total != 5 {
		t.Fatal("background allowance", grants, err)
	}
}
