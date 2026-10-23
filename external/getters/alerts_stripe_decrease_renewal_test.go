package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Historical consent is seeded so the actual clock is just past renewal. All
// renewal observations and invoices pass through the Stripe SDK HTTP adapter.
func TestAlertsStripeDecreaseRenewalHTTPPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Decrease renewal fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	}()
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	checkout, err := ReserveAlertsStripeCheckout(app, person, uuid.NewString(), 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	id := checkout.Intent.ID
	b := alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, CustomerID: "cus_" + id, PriceID: "price_" + id, ProductID: "prod_" + id, PaidSlots: 2}
	raw, _ := json.Marshal(b)
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, id, b.SubscriptionID, raw); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	due := now.Add(-time.Hour)
	start := due.AddDate(-1, 0, 0)
	end := due.AddDate(1, 0, 0)
	prior := alertsstripe.PaidPeriod{InvoiceID: "in_prior_" + id, SubscriptionID: b.SubscriptionID, Quantity: 2, TotalCents: 4800, Start: start, End: due, PaidAt: start}
	receipt, err := recordAlertsStripeInvoice(app, b, prior)
	if err != nil {
		t.Fatal(err)
	}
	oldState := alertsstripe.SubscriptionState{State: "active", ProviderStatus: "active", PeriodStart: start.Unix(), PeriodEnd: due.Unix(), LatestInvoice: prior.InvoiceID}
	raw, _ = json.Marshal(oldState)
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscription_state(checkout_id,revision,snapshot) VALUES($1,1,$2)`, id, raw); err != nil {
		t.Fatal(err)
	}
	quote := uuid.NewString()
	sid := "sub_sched_" + id
	q := alertsstripe.DecreasePreview{PreviousSlots: 2, PaidSlots: 1, AnnualCents: 2400, PeriodStart: start.Unix(), EffectiveAt: due.Unix(), ExpiresAt: due.Add(-time.Hour)}
	raw, _ = json.Marshal(q)
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_decrease_quotes(id,checkout_id,revision,preview,created_at,expires_at) VALUES($1,$2,1,$3,$4,$5)`, quote, id, raw, q.ExpiresAt.Add(-10*time.Minute), q.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_decreases(quote_id,checkout_id,state) VALUES($1,$2,'scheduled')`, quote, id); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_decrease_attachments(quote_id,schedule_id,lease_until) VALUES($1,$2,now())`, quote, sid); err != nil {
		t.Fatal(err)
	}
	price := &stripe.Price{ID: b.PriceID, Currency: "usd", UnitAmount: 2400, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "year", IntervalCount: 1}, Product: &stripe.Product{ID: b.ProductID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	meta := map[string]string{"account_id": person, "checkout_intent": id, "product": "alerts.repository-slots"}
	method := stripe.SubscriptionCollectionMethod("charge_automatically")
	schedule := &stripe.SubscriptionSchedule{ID: sid, Status: "active", EndBehavior: "release", Customer: &stripe.Customer{ID: b.CustomerID}, Subscription: &stripe.Subscription{ID: b.SubscriptionID}, CurrentPhase: &stripe.SubscriptionScheduleCurrentPhase{StartDate: start.Unix(), EndDate: due.Unix()}, Metadata: map[string]string{"account_id": person, "checkout_intent": id, "alerts_decrease": quote}}
	for i, quantity := range []int64{2, 1} {
		at, until := start, due
		if i == 1 {
			at = due
			until = end
		}
		schedule.Phases = append(schedule.Phases, &stripe.SubscriptionSchedulePhase{StartDate: at.Unix(), EndDate: until.Unix(), Currency: "usd", CollectionMethod: &method, AutomaticTax: &stripe.SubscriptionAutomaticTax{Enabled: true}, ProrationBehavior: "none", Metadata: meta, Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: price, Quantity: quantity}}})
	}
	raw, _ = json.Marshal(schedule)
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_decrease_schedules(quote_id,schedule_id,lease_until,snapshot,verified_at) VALUES($1,$2,now(),$3,$4)`, quote, sid, raw, q.ExpiresAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	schedule.CurrentPhase = &stripe.SubscriptionScheduleCurrentPhase{StartDate: due.Unix(), EndDate: end.Unix()}
	makeInvoice := func(id string, quantity int64, from, to time.Time) *stripe.Invoice {
		return &stripe.Invoice{ID: id, Status: "paid", Currency: "usd", Customer: &stripe.Customer{ID: b.CustomerID}, BillingReason: "subscription_cycle", Total: quantity * 2400, AmountPaid: quantity * 2400, CollectionMethod: "charge_automatically", Attempted: true, AttemptCount: 1, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: from.Unix()}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: &stripe.Subscription{ID: b.SubscriptionID}, Metadata: meta}}, Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{{Currency: "usd", Quantity: quantity, Amount: quantity * 2400, Period: &stripe.Period{Start: from.Unix(), End: to.Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: b.SubscriptionID}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: b.ProductID}}}}}}
	}
	base := makeInvoice(prior.InvoiceID, 2, start, due)
	renewal := makeInvoice("in_renewal_"+id, 1, due, end)
	renewal.Status = "open"
	renewal.AmountPaid = 0
	renewal.AmountRemaining = 2400
	renewal.StatusTransitions = nil
	sub := &stripe.Subscription{ID: b.SubscriptionID, Status: "past_due", Customer: &stripe.Customer{ID: b.CustomerID}, Metadata: meta, Schedule: &stripe.SubscriptionSchedule{ID: sid}, CollectionMethod: "charge_automatically", LatestInvoice: &stripe.Invoice{ID: renewal.ID}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: 1, Price: price, CurrentPeriodStart: due.Unix(), CurrentPeriodEnd: end.Unix()}}}}
	var mu sync.Mutex
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "GET" {
			t.Error("renewal reconciliation mutated provider", r.Method, r.URL)
			http.Error(w, "read only", 405)
			return
		}
		switch r.URL.Path {
		case "/v1/subscription_schedules/" + sid:
			json.NewEncoder(w).Encode(schedule)
		case "/v1/subscriptions/" + b.SubscriptionID:
			json.NewEncoder(w).Encode(sub)
		case "/v1/prices/" + b.PriceID:
			json.NewEncoder(w).Encode(price)
		case "/v1/invoices/" + base.ID:
			json.NewEncoder(w).Encode(base)
		case "/v1/invoices/" + renewal.ID:
			json.NewEncoder(w).Encode(renewal)
		case "/v1/invoices":
			invoices := []*stripe.Invoice{base}
			if renewal.Status == "paid" {
				invoices = append(invoices, renewal)
			}
			json.NewEncoder(w).Encode(stripe.InvoiceList{Data: invoices})
		default:
			t.Error("unexpected lookup", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	backend, key := stripe.GetBackend(stripe.APIBackend), stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	stripe.Key = "sk_test_decrease_renewal"
	defer func() { stripe.SetBackend(stripe.APIBackend, backend); stripe.Key = key }()
	client := alertsstripe.NewClient(false)
	for i := 0; i < 2; i++ {
		if err = RecoverAlertsStripePayments(ctx, app, person, id, client); err != nil {
			t.Fatal("failed renewal recovery", err)
		}
	}
	var current int64
	var status string
	var graceSlots int64
	var graceDue time.Time
	if err = app.DB.QueryRow(ctx, `SELECT (binding->>'PaidSlots')::bigint FROM alerts_stripe_current_subscriptions WHERE checkout_id=$1`, id).Scan(&current); err != nil || current != 1 {
		t.Fatal(current, err)
	}
	if err = app.DB.QueryRow(ctx, `SELECT state,paid_slots,due_at FROM alerts_stripe_renewals WHERE invoice_id=$1`, renewal.ID).Scan(&status, &graceSlots, &graceDue); err != nil || status != "failed" || graceSlots != 1 || !graceDue.Equal(due) {
		t.Fatal(status, graceSlots, graceDue, err)
	}
	grants, err := ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 0 {
		t.Fatal("unpaid renewal granted paid access", grants, err)
	}
	mu.Lock()
	renewal.Status = "paid"
	renewal.AmountPaid = 2400
	renewal.AmountRemaining = 0
	renewal.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}
	sub.Status = "active"
	mu.Unlock()
	for i := 0; i < 2; i++ {
		if err = RecoverAlertsStripePayments(ctx, app, person, id, client); err != nil {
			t.Fatal("paid renewal recovery", err)
		}
	}
	grants, err = ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 1 || !grants[0].EndsAt.Equal(end) {
		t.Fatal(grants, err)
	}
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_renewals WHERE invoice_id=$1`, renewal.ID).Scan(&status); err != nil || status != "recovered" {
		t.Fatal("grace not settled", status, err)
	}
	replay, err := ReconcileAlertsStripeInvoice(ctx, app, person, id, base.ID, client)
	if err != nil || replay.OrderID != receipt.OrderID {
		t.Fatal("historical invoice replay", replay, err)
	}
	var transitions, invoices int
	if err = app.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM alerts_stripe_quantity_history WHERE checkout_id=$1),(SELECT count(*) FROM alerts_stripe_invoices WHERE checkout_id=$1)`, id).Scan(&transitions, &invoices); err != nil || transitions != 1 || invoices != 2 {
		t.Fatal("duplicate facts", transitions, invoices, err)
	}
}
