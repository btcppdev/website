package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v86"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServiceStripeEventRouting(t *testing.T) {
	for _, tc := range []struct {
		name             string
		kind             stripe.EventType
		raw              string
		handled          bool
		session, invoice string
	}{
		{"subscription binds only", "checkout.session.completed", `{"id":"cs_test","mode":"subscription","metadata":{"product":"insider.membership.monthly","checkout_intent":"intent"},"invoice":"in_not_proof"}`, true, "cs_test", ""},
		{"annual invoice", "invoice.paid", `{"id":"in_test","parent":{"subscription_details":{"metadata":{"product":"insider.membership.annual","checkout_intent":"intent"}}}}`, true, "", "in_test"},
		{"fixed term unchanged", "checkout.session.completed", `{"id":"cs_fixed","mode":"payment","metadata":{"product":"insider.membership.annual","service-attempt-id":"attempt"}}`, false, "", ""},
		{"Alerts separate", "invoice.paid", `{"id":"in_alerts","parent":{"subscription_details":{"metadata":{"product":"alerts.repository-slots","checkout_intent":"intent"}}}}`, false, "", ""},
		{"article unchanged", "checkout.session.completed", `{"id":"cs_article","mode":"payment","metadata":{"product":"insider.article"}}`, false, "", ""},
		{"failure grants nothing", "invoice.payment_failed", `{"id":"in_failed"}`, false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref, handled, err := serviceStripeEventReference(stripe.Event{Type: tc.kind, Data: &stripe.EventData{Raw: json.RawMessage(tc.raw)}})
			if err != nil || handled != tc.handled || ref.Session != tc.session || ref.Invoice != tc.invoice || (handled && ref.Intent != "intent") {
				t.Fatal(ref, handled, err)
			}
		})
	}
}

func TestUnsignedServiceStripeInvoiceCannotReachDatabase(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{StripeEndpointSec: "whsec_local_test"}, Err: log.New(io.Discard, "", 0)}
	req := httptest.NewRequest("POST", "/callback/stripe", strings.NewReader(`{"id":"evt_forged","type":"invoice.paid","data":{"object":{"id":"in_forged","parent":{"subscription_details":{"metadata":{"product":"insider.membership.monthly","checkout_intent":"intent"}}}}}}`))
	out := httptest.NewRecorder()
	StripeCallback(out, req, app)
	if out.Code != 400 {
		t.Fatal("unsigned invoice accepted", out.Code)
	}
}

func TestServiceStripeEventPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" || os.Getenv("DATABASE_URL") == "" {
		t.Skip("PostgreSQL smoke disabled")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool}
	var person string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Insider recurring event test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "events-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	terms := servicestripe.CheckoutIntent{ProductCode: "insider.membership.monthly", Quantity: 1, TaxCode: "txcd_test", ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/account/success", CancelURL: "https://insider.btcpp.dev/account/cancel"}
	reserved, err := getters.ReserveServiceStripeCheckout(app, person, "insider-event-checkout", terms)
	if err != nil {
		t.Fatal(err)
	}
	id := reserved.Intent.ID
	meta := map[string]string{"product": terms.ProductCode, "account_id": person, "checkout_intent": id}
	price := &stripe.Price{ID: "price_" + id, Currency: "usd", UnitAmount: 2100, TaxBehavior: "exclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "month", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_" + id, TaxCode: &stripe.TaxCode{ID: terms.TaxCode}}}
	session := &stripe.CheckoutSession{ID: "cs_" + id, Mode: "subscription", Status: "open", ExpiresAt: time.Now().Add(time.Hour).Unix(), ClientReferenceID: id, Metadata: meta, Customer: &stripe.Customer{ID: "cus_" + person}, Subscription: &stripe.Subscription{ID: "sub_" + id}, LineItems: &stripe.LineItemList{Data: []*stripe.LineItem{{Quantity: 1, Price: price}}}}
	session.URL = "https://checkout.stripe.com/c/pay/" + session.ID
	checkout := servicestripe.CheckoutClient{Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }}
	app.Env = &types.EnvConfig{InsiderStripeTaxCode: terms.TaxCode, InsiderAllowedReturnOrigins: "https://insider.btcpp.dev", InsiderFulfillmentEndpoint: "https://insider.btcpp.dev/api/internal/commerce/open-access", InsiderLifecycleEndpoint: "https://insider.btcpp.dev/api/internal/commerce/subscription-state", InsiderFulfillmentToken: "test-fulfillment", InsiderMerchantName: "Test merchant"}
	checkoutRequest := insiderCheckoutRequest{BillingMode: "recurring", RequestID: "insider-event-checkout", PersonID: person, ProductCode: terms.ProductCode, PaymentProvider: "stripe", AmountCents: 2100, Currency: "USD", QuoteExpiresAt: time.Now().Add(10 * time.Minute), SuccessURL: terms.SuccessURL, CancelURL: terms.CancelURL}
	for i := 0; i < 2; i++ {
		out := httptest.NewRecorder()
		serveInsiderRecurringCheckout(out, httptest.NewRequest("POST", "/api/internal/insider/checkouts", nil), app, checkoutRequest, checkout)
		var result insiderCheckoutResponse
		if out.Code != 201 || json.Unmarshal(out.Body.Bytes(), &result) != nil || result.CheckoutID != id || result.CheckoutURL != session.URL || result.OrderID != "" {
			t.Fatal("recurring adapter or replay", out.Code, out.Body.String())
		}
	}
	session.Status = "complete"
	now := time.Now().UTC().Truncate(time.Second)
	inv := &stripe.Invoice{ID: "in_" + id, Status: "paid", Currency: "usd", Customer: session.Customer, BillingReason: "subscription_create", Total: 2100, AmountPaid: 2100, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: session.Subscription, Metadata: meta}}, Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{{Currency: "usd", Amount: 2100, Quantity: 1, Period: &stripe.Period{Start: now.Unix(), End: now.AddDate(0, 1, 0).Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: session.Subscription.ID}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: price.Product.ID}}}}}}
	calls := 0
	invoices := servicestripe.InvoiceClient{Invoice: func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { calls++; return inv, nil }, Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return price, nil }}
	raw, _ := json.Marshal(inv)
	event := stripe.Event{Type: "invoice.paid", Data: &stripe.EventData{Raw: raw}}

	session.Invoice = &stripe.Invoice{ID: inv.ID}
	if err = getters.RecoverServiceStripeCheckout(ctx, app, person, id, terms.ProductCode, checkout, invoices); err != nil {
		t.Fatal("missed callback recovery", err)
	}
	for i := 0; i < 2; i++ {
		if handled, err := processServiceStripeEvent(ctx, app, event, checkout, invoices, servicestripe.SubscriptionClient{}); !handled || err != nil {
			t.Fatal("invoice before completion", handled, err)
		}
	}
	raw, _ = json.Marshal(session)
	if handled, err := processServiceStripeEvent(ctx, app, stripe.Event{Type: "checkout.session.completed", Data: &stripe.EventData{Raw: raw}}, checkout, invoices, servicestripe.SubscriptionClient{}); !handled || err != nil {
		t.Fatal("late completion", handled, err)
	}
	sub := &stripe.Subscription{ID: session.Subscription.ID, Customer: session.Customer, Status: "active", Metadata: meta, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: 1, Price: price, CurrentPeriodStart: now.Unix(), CurrentPeriodEnd: now.AddDate(0, 1, 0).Unix()}}}}
	subscriptions := servicestripe.SubscriptionClient{Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }}
	stale := *sub
	stale.Status = "canceled"
	staleRaw, _ := json.Marshal(stale)
	lifecycle := stripe.Event{Type: "customer.subscription.deleted", Data: &stripe.EventData{Raw: staleRaw}}
	if handled, err := processServiceStripeEvent(ctx, app, lifecycle, checkout, invoices, subscriptions); !handled || err != nil {
		t.Fatal("lifecycle refresh", handled, err)
	}
	var state string
	if err = pool.QueryRow(ctx, `SELECT snapshot->>'state' FROM service_stripe_subscription_state WHERE checkout_id=$1`, id).Scan(&state); err != nil || state != "active" {
		t.Fatal("stale webhook replaced current state", state, err)
	}
	sub.PauseCollection = &stripe.SubscriptionPauseCollection{Behavior: "void"}
	failed := stripe.Event{Type: "invoice.payment_failed", Data: event.Data}
	if handled, err := processServiceStripeEvent(ctx, app, failed, checkout, invoices, subscriptions); !handled || err != nil {
		t.Fatal("payment failure refresh", handled, err)
	}
	if err = pool.QueryRow(ctx, `SELECT snapshot->>'state' FROM service_stripe_subscription_state WHERE checkout_id=$1`, id).Scan(&state); err != nil || state != "paused" {
		t.Fatal("pause confused with renewal failure", state, err)
	}

	app.Env = &types.EnvConfig{InsiderServiceToken: "private-management-test"}
	management := insiderSubscriptionRequest{PersonID: person, CheckoutID: id, ProductCode: terms.ProductCode}
	sendManagement := func(cancel bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(management)
		request := httptest.NewRequest("POST", "/api/internal/insider/subscriptions/status", strings.NewReader(string(body)))
		request.Header.Set("Authorization", "Bearer private-management-test")
		request.Header.Set("Idempotency-Key", management.RequestID)
		response := httptest.NewRecorder()
		serveInsiderSubscription(response, request, app, cancel, &subscriptions)
		return response
	}
	response := sendManagement(false)
	var summary getters.ServiceSubscriptionStatus
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &summary) != nil || summary.State.State != "paused" || summary.State.LatestInvoice != "" {
		t.Fatal("management status", response.Code, response.Body.String())
	}

	management.CheckoutID = ""
	management.ProductCode = ""
	discovered := sendManagement(false)
	var current getters.ServiceSubscriptionStatus
	if discovered.Code != 200 || json.Unmarshal(discovered.Body.Bytes(), &current) != nil || current.CheckoutID != id || current.ProductCode != terms.ProductCode {
		t.Fatal("account subscription discovery", discovered.Code, discovered.Body.String())
	}
	management.CheckoutID = id
	management.ProductCode = terms.ProductCode
	management.RequestID = id
	management.Revision = summary.Revision
	management.PeriodEnd = summary.State.PeriodEnd
	mutations := 0
	subscriptions.UpdateSubscription = func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) {
		mutations++
		sub.CancelAtPeriodEnd = true
		return nil, errors.New("ambiguous provider response")
	}
	response = sendManagement(true)
	if response.Code != 202 || !strings.Contains(response.Body.String(), `"state":"pending"`) {
		t.Fatal("durable pending response", response.Code, response.Body.String())
	}
	response = sendManagement(true)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"state":"confirmed"`) || mutations != 1 {
		t.Fatal("confirmed cancellation replay", response.Code, response.Body.String(), mutations)
	}
	management.PersonID = "00000000-0000-0000-0000-000000000001"
	response = sendManagement(true)
	if response.Code != 404 || mutations != 1 {
		t.Fatal("foreign account cancellation", response.Code, mutations)
	}

	management = insiderSubscriptionRequest{PersonID: "00000000-0000-0000-0000-000000000001"}
	response = sendManagement(false)
	if response.Code != 404 {
		t.Fatal("discovery exposed another account", response.Code, response.Body.String())
	}

	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM service_fulfillment_jobs j JOIN service_payment_attempts a ON a.id=j.attempt_id JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.person_id=$1`, person).Scan(&count); err != nil || count != 1 {
		t.Fatal("fulfillment not deduplicated", count, err)
	}
	// Exercise the actual database-backed worker after verified provider events,
	// including an outage and retry, rather than testing HTTP transport alone.
	var deliveries [][]byte
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var delivered getters.ServiceLifecycleEvent
		if json.Unmarshal(body, &delivered) != nil || delivered.CheckoutID != id || delivered.PersonID != person || r.Header.Get("Authorization") != "Bearer lifecycle-secret" || r.Header.Get("Idempotency-Key") != delivered.EventID {
			t.Error("incorrect lifecycle envelope")
			w.WriteHeader(400)
			return
		}
		deliveries = append(deliveries, body)
		if len(deliveries) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	app.Env.InsiderLifecycleEndpoint, app.Env.InsiderFulfillmentToken = receiver.URL, "lifecycle-secret"
	deliveryAt := time.Now().Add(time.Second)
	if worked, err := processServiceLifecycle(ctx, app, id, deliveryAt); !worked || err == nil {
		t.Fatal("outage acknowledged lifecycle event", worked, err)
	}
	if worked, err := processServiceLifecycle(ctx, app, id, deliveryAt.Add(30*time.Second)); worked || err != nil {
		t.Fatal("later revision bypassed backoff", worked, err)
	}
	if worked, err := processServiceLifecycle(ctx, app, id, deliveryAt.Add(time.Minute)); !worked || err != nil {
		t.Fatal("lifecycle retry failed", worked, err)
	}
	if len(deliveries) != 2 || string(deliveries[0]) != string(deliveries[1]) {
		t.Fatal("retry changed lifecycle payload")
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM service_lifecycle_jobs WHERE checkout_id=$1 AND revision=1`, id).Scan(&state); err != nil || state != "accepted" {
		t.Fatal("receiver acknowledgment not saved", state, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
		t.Fatal(err)
	}
	if worked, err := processServiceLifecycle(ctx, app, id, deliveryAt.Add(2*time.Minute)); !worked || err == nil || len(deliveries) != 2 {
		t.Fatal("deleted account event sent", worked, len(deliveries), err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM service_lifecycle_jobs WHERE checkout_id=$1 AND revision=2`, id).Scan(&state); err != nil || state != "review" {
		t.Fatal("deleted account not held for review", state, err)
	}
	if handled, err := processServiceStripeEvent(ctx, app, event, checkout, invoices, servicestripe.SubscriptionClient{}); !handled || err != nil {
		t.Fatal("deleted account", handled, err)
	}

	var attempt string
	if err = pool.QueryRow(ctx, `SELECT a.id::text FROM service_payment_attempts a WHERE a.provider_reference=$1`, inv.ID).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	if _, err = getters.GetFulfillableServicePaymentContext(app, attempt); !errors.Is(err, getters.ErrServicePayment) {
		t.Fatal("deleted account remained fulfillable", err)
	}
	if calls != 3 {
		t.Fatal("deleted account reached provider", calls)
	}
}

func TestServiceStripeExpirationPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" || os.Getenv("DATABASE_URL") == "" {
		t.Skip("PostgreSQL smoke disabled")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool}
	var person string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Insider recurring event test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "events-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	terms := servicestripe.CheckoutIntent{ProductCode: "insider.membership.monthly", Quantity: 1, TaxCode: "txcd_test", ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/account/success", CancelURL: "https://insider.btcpp.dev/account/cancel"}
	reserved, err := getters.ReserveServiceStripeCheckout(app, person, "insider-event-checkout", terms)
	if err != nil {
		t.Fatal(err)
	}

	id := reserved.Intent.ID
	session := &stripe.CheckoutSession{ID: "cs_" + id, Mode: "subscription", Status: "expired", PaymentStatus: "unpaid", ClientReferenceID: id, Metadata: map[string]string{"product": terms.ProductCode, "account_id": person, "checkout_intent": id}}
	checkout := servicestripe.CheckoutClient{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }}
	raw, _ := json.Marshal(session)
	event := stripe.Event{Type: "checkout.session.expired", Data: &stripe.EventData{Raw: raw}}
	session.Status = "open"
	if handled, err := processServiceStripeEvent(ctx, app, event, checkout, servicestripe.InvoiceClient{}, servicestripe.SubscriptionClient{}); !handled || err == nil {
		t.Fatal("webhook alone closed checkout", handled, err)
	}
	session.Status = "expired"
	for i := 0; i < 2; i++ {
		if handled, err := processServiceStripeEvent(ctx, app, event, checkout, servicestripe.InvoiceClient{}, servicestripe.SubscriptionClient{}); !handled || err != nil {
			t.Fatal("expiry failed", handled, err)
		}
	}
	stored, err := getters.ServiceStripeCheckoutForPerson(app, person, id, terms.ProductCode)
	if err != nil || !stored.Closed || stored.Reference != session.ID {
		t.Fatal("lost response reference not preserved", stored, err)
	}
	next, err := getters.ReserveServiceStripeCheckout(app, person, "replacement-checkout-key", terms)
	if err != nil || next.Intent.ID == id {
		t.Fatal("reservation not released", next, err)
	}
	session.ClientReferenceID = next.Intent.ID
	session.Metadata["checkout_intent"] = next.Intent.ID
	session.ID = "cs_" + next.Intent.ID
	binding := `{"test":true}`
	if _, err = pool.Exec(ctx, `INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, next.Intent.ID, "sub_"+next.Intent.ID, binding); err != nil {
		t.Fatal(err)
	}
	if err = getters.CloseExpiredServiceStripeCheckout(ctx, app, person, next.Intent.ID, terms.ProductCode, session.ID, checkout); err == nil {
		t.Fatal("bound subscription released")
	}
}
