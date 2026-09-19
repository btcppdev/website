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
	if _, err = getters.OpenServiceStripeCheckout(ctx, app, person, id, terms.ProductCode, checkout); err != nil {
		t.Fatal(err)
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
		if handled, err := processServiceStripeEvent(ctx, app, event, checkout, invoices); !handled || err != nil {
			t.Fatal("invoice before completion", handled, err)
		}
	}
	raw, _ = json.Marshal(session)
	if handled, err := processServiceStripeEvent(ctx, app, stripe.Event{Type: "checkout.session.completed", Data: &stripe.EventData{Raw: raw}}, checkout, invoices); !handled || err != nil {
		t.Fatal("late completion", handled, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM service_fulfillment_jobs j JOIN service_payment_attempts a ON a.id=j.attempt_id JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.person_id=$1`, person).Scan(&count); err != nil || count != 1 {
		t.Fatal("fulfillment not deduplicated", count, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
		t.Fatal(err)
	}
	if handled, err := processServiceStripeEvent(ctx, app, event, checkout, invoices); !handled || err != nil {
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
	if handled, err := processServiceStripeEvent(ctx, app, event, checkout, servicestripe.InvoiceClient{}); !handled || err == nil {
		t.Fatal("webhook alone closed checkout", handled, err)
	}
	session.Status = "expired"
	for i := 0; i < 2; i++ {
		if handled, err := processServiceStripeEvent(ctx, app, event, checkout, servicestripe.InvoiceClient{}); !handled || err != nil {
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
