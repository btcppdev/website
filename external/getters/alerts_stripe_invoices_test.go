package getters

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
)

func TestAlertsStripeInvoiceActivationPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Stripe invoice activation') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "invoice-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	reserved, err := ReserveAlertsStripeCheckout(app, person, "invoice-activation-key", 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	price := &stripe.Price{ID: "price_" + reserved.Intent.ID, Currency: "usd", UnitAmount: 200, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "month", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_" + reserved.Intent.ID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	session := &stripe.CheckoutSession{ID: "cs_test_" + reserved.Intent.ID, Status: "open", Mode: "subscription", ExpiresAt: time.Now().Add(time.Hour).Unix(), ClientReferenceID: reserved.Intent.ID, Metadata: map[string]string{"product": "alerts.repository-slots", "account_id": person, "checkout_intent": reserved.Intent.ID}, Customer: &stripe.Customer{ID: "cus_" + person}, Subscription: &stripe.Subscription{ID: "sub_" + reserved.Intent.ID}, LineItems: &stripe.LineItemList{Data: []*stripe.LineItem{{Quantity: 2, Price: price}}}}
	session.URL = "https://checkout.stripe.com/c/pay/" + session.ID
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Hour)
	end := start.AddDate(0, 1, 0)
	inv := &stripe.Invoice{ID: "in_" + reserved.Intent.ID, Status: "paid", Currency: "usd", Customer: session.Customer, BillingReason: "subscription_create", Total: 400, AmountPaid: 400, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: session.Subscription, Metadata: session.Metadata}}, TotalTaxes: []*stripe.InvoiceTotalTax{{Amount: 36, TaxBehavior: "inclusive"}}, Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{{Currency: "usd", Amount: 400, Quantity: 2, Period: &stripe.Period{Start: start.Unix(), End: end.Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: session.Subscription.ID}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: price.Product.ID}}}}}}
	client := alertsstripe.Client{Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Invoice: func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return inv, nil }, Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return price, nil }}
	// Simulate a lost creation response: the session exists at Stripe but its
	// reference was never saved locally. A verified completion can recover it.
	if _, err := ResolveAlertsStripeSubscription(ctx, app, person, reserved.Intent.ID, client); err == nil {
		t.Fatal("unfinished checkout bound")
	}
	session.Status = "complete"
	if _, err := ResolveAlertsStripeSubscription(ctx, app, person, reserved.Intent.ID, client, session.ID); err != nil {
		t.Fatal(err)
	}
	grants, err := ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 0 {
		t.Fatal("binding alone granted access", grants, err)
	}
	session.Invoice = &stripe.Invoice{ID: inv.ID}
	if err = RecoverAlertsStripeCheckout(ctx, app, person, reserved.Intent.ID, client); err != nil {
		t.Fatal("missed payment callback recovery", err)
	}
	grants, err = ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 2 {
		t.Fatal("recovery did not activate verified invoice", grants, err)
	}
	const n = 5
	var wg sync.WaitGroup
	results := make(chan *ServicePurchase, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			order, err := ReconcileAlertsStripeInvoice(ctx, app, person, reserved.Intent.ID, inv.ID, client)
			results <- order
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *ServicePurchase
	for out := range results {
		if first != nil && out.OrderID != first.OrderID {
			t.Fatal("duplicate order")
		}
		first = out
	}
	grants, err = ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 2 || !grants[0].EndsAt.Equal(end) {
		t.Fatal("paid allowance", grants, err)
	}
	sub := &stripe.Subscription{ID: session.Subscription.ID, Customer: session.Customer, Metadata: session.Metadata, Status: "active", Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: 2, Price: price, CurrentPeriodStart: start.Unix(), CurrentPeriodEnd: end.Unix()}}}}
	client.Subscription = func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }
	for i := 0; i < 2; i++ {
		if err = RefreshAlertsStripeSubscription(ctx, app, person, reserved.Intent.ID, client); err != nil {
			t.Fatal(err)
		}
	}
	requestID := uuid.NewString()
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, reserved.Intent.ID, requestID, 999, end.Unix()); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale cancellation review accepted", err)
	}
	cancellation, err := ReserveAlertsStripeCancellation(ctx, app, person, reserved.Intent.ID, requestID, 1, end.Unix())
	if err != nil {
		t.Fatal(err)
	}
	other, err := ReserveAlertsStripeCancellation(ctx, app, person, reserved.Intent.ID, uuid.NewString(), 1, end.Unix())
	if err != nil || other.Intent.ID != cancellation.Intent.ID {
		t.Fatal("duplicate pending instruction", other, err)
	}
	if _, err = ProcessAlertsStripeCancellation(ctx, app, "00000000-0000-0000-0000-000000000000", requestID, client); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("cross-account cancellation", err)
	}
	updates := 0
	lost := errors.New("response lost after provider applied cancellation")
	client.UpdateSubscription = func(id string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
		updates++
		var saved string
		if e := app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_cancellations WHERE id=$1`, requestID).Scan(&saved); e != nil || saved != "pending" {
			t.Fatal("provider called without committed consent", saved, e)
		}
		if p.IdempotencyKey == nil || *p.IdempotencyKey != "alerts-cancel:"+requestID {
			t.Fatal("wrong cancellation key")
		}
		sub.CancelAtPeriodEnd = true
		return nil, lost
	}
	if _, err = ProcessAlertsStripeCancellation(ctx, app, person, requestID, client); !errors.Is(err, lost) {
		t.Fatal("ambiguous response lost", err)
	}
	var persisted string
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_cancellations WHERE id=$1`, requestID).Scan(&persisted); err != nil || persisted != "pending" {
		t.Fatal("lost pending request", persisted, err)
	}
	client.ListInvoices = func(*stripe.InvoiceListParams) (*stripe.InvoiceList, error) { return &stripe.InvoiceList{}, nil }
	if err = RecoverAlertsStripePayments(ctx, app, person, reserved.Intent.ID, client); err != nil {
		t.Fatal("background payment recovery did not reconcile cancellation", err)
	}
	for i := 0; i < 2; i++ {
		if outcome, e := ProcessAlertsStripeCancellation(ctx, app, person, requestID, client); e != nil || outcome != "confirmed" {
			t.Fatal(outcome, e)
		}
	}
	if updates != 1 {
		t.Fatal("recovery sent another mutation", updates)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, reserved.Intent.ID, requestID, 1, end.Unix()); err != nil {
		t.Fatal("repeated consent lost original result", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_cancellations SET state='pending' WHERE id=$1`, requestID); err == nil {
		t.Fatal("reopened confirmed cancellation")
	}
	var revision int64
	var state string
	if err = app.DB.QueryRow(ctx, `SELECT revision,snapshot->>'state' FROM alerts_stripe_subscription_state WHERE checkout_id=$1`, reserved.Intent.ID).Scan(&revision, &state); err != nil || revision != 2 || state != "cancelling" {
		t.Fatal(revision, state, err)
	}
	grants, err = ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || !grants[0].EndsAt.Equal(end) {
		t.Fatal("scheduled cancellation changed paid-through access", grants, err)
	}
	if _, err := ReconcileAlertsStripeInvoice(ctx, app, "00000000-0000-0000-0000-000000000000", reserved.Intent.ID, inv.ID, client); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("cross-account invoice", err)
	}
	// A later renewal is a new purchase, while the previous interval remains intact.
	inv.ID += "_renewal"
	inv.BillingReason = "subscription_cycle"
	inv.Lines.Data[0].Period = &stripe.Period{Start: end.Unix(), End: end.AddDate(0, 1, 0).Unix()}
	// Failed renewals retain their original due time through retries and pauses.
	sub.Status = "past_due"
	sub.CancelAtPeriodEnd = false
	sub.LatestInvoice = &stripe.Invoice{ID: inv.ID}
	sub.Items.Data[0].CurrentPeriodStart = end.Unix()
	sub.Items.Data[0].CurrentPeriodEnd = inv.Lines.Data[0].Period.End
	inv.Status = "open"
	inv.Attempted = true
	inv.AttemptCount = 1
	inv.CollectionMethod = "charge_automatically"
	inv.AmountPaid = 0
	inv.AmountRemaining = 400
	inv.StatusTransitions.PaidAt = 0
	for i := 0; i < 2; i++ {
		if err = refreshAlertsStripeSubscriptionAt(ctx, app, person, reserved.Intent.ID, client, end.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	var due time.Time
	access, accessErr := AccountAccess(ctx, app, person, end.Add(time.Hour))
	if accessErr != nil || access.AccountID != person || len(access.Renewals) != 1 || access.Renewals[0].State != "failed" || access.Renewals[0].RenewalID != inv.ID || !access.Renewals[0].DueAt.Equal(end) || access.Renewals[0].PaidSlots != 2 || len(access.Entitlements) != 0 {
		t.Fatal("inconsistent renewal/access snapshot", access, accessErr)
	}
	otherAccess, accessErr := AccountAccess(ctx, app, "00000000-0000-0000-0000-000000000000", end.Add(time.Hour))
	if accessErr != nil || len(otherAccess.Renewals) != 0 || len(otherAccess.Entitlements) != 0 {
		t.Fatal("cross-account renewal evidence", otherAccess, accessErr)
	}
	if err = app.DB.QueryRow(ctx, `SELECT state,revision,due_at FROM alerts_stripe_renewals WHERE invoice_id=$1`, inv.ID).Scan(&state, &revision, &due); err != nil || state != "failed" || revision != 1 || !due.Equal(end) {
		t.Fatal(state, revision, due, err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_renewals SET due_at=due_at+interval '1 hour' WHERE invoice_id=$1`, inv.ID); err == nil {
		t.Fatal("renewal due time was mutable")
	}
	sub.PauseCollection = &stripe.SubscriptionPauseCollection{Behavior: "void"}
	if err = refreshAlertsStripeSubscriptionAt(ctx, app, person, reserved.Intent.ID, client, end.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sub.PauseCollection = nil
	if err = refreshAlertsStripeSubscriptionAt(ctx, app, person, reserved.Intent.ID, client, end.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = app.DB.QueryRow(ctx, `SELECT state,revision FROM alerts_stripe_renewals WHERE invoice_id=$1`, inv.ID).Scan(&state, &revision); err != nil || state != "paused" || revision != 2 {
		t.Fatal("resolved failure reopened", state, revision, err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_renewals SET state='failed',revision=revision+1 WHERE invoice_id=$1`, inv.ID); err == nil {
		t.Fatal("database allowed resolved renewal to reopen")
	}
	sub.Status = "active"
	inv.Status = "paid"
	inv.AmountPaid = 400
	inv.AmountRemaining = 0
	inv.StatusTransitions.PaidAt = now.Unix()
	invoiceReads := 0
	client.Invoice = func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { invoiceReads++; return inv, nil }
	client.ListInvoices = func(p *stripe.InvoiceListParams) (*stripe.InvoiceList, error) {
		if p.StartingAfter == nil {
			return &stripe.InvoiceList{ListMeta: stripe.ListMeta{HasMore: true}, Data: []*stripe.Invoice{{ID: "in_" + reserved.Intent.ID, Status: "paid", Customer: session.Customer}}}, nil
		}
		if *p.StartingAfter != "in_"+reserved.Intent.ID {
			t.Fatal("unexpected recovery cursor", *p.StartingAfter)
		}
		return &stripe.InvoiceList{Data: []*stripe.Invoice{inv}}, nil
	}
	for i := 0; i < 2; i++ {
		if err = RecoverAlertsStripePayments(ctx, app, person, reserved.Intent.ID, client); err != nil {
			t.Fatal(err)
		}
	}
	if invoiceReads != 1 {
		t.Fatal("already recorded invoice was reprocessed", invoiceReads)
	}
	if err = app.DB.QueryRow(ctx, `SELECT state,revision FROM alerts_stripe_renewals WHERE invoice_id=$1`, inv.ID).Scan(&state, &revision); err != nil || state != "recovered" || revision != 3 {
		t.Fatal("payment did not resolve renewal", state, revision, err)
	}
	grants, err = ActiveServiceEntitlements(app, person, end)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 2 {
		t.Fatal("renewal stacked grants", grants, err)
	}
	inv.ID += "_overlap"
	if _, err := ReconcileAlertsStripeInvoice(ctx, app, person, reserved.Intent.ID, inv.ID, client); err == nil {
		t.Fatal("overlapping billing period accepted")
	}
	// Replaying an already recorded invoice cannot undo a refund.
	if _, err := app.DB.Exec(ctx, `UPDATE shop_orders SET status='refunded' WHERE id=$1`, first.OrderID); err != nil {
		t.Fatal(err)
	}
	inv.ID = "in_" + reserved.Intent.ID
	inv.BillingReason = "subscription_create"
	inv.Lines.Data[0].Period = &stripe.Period{Start: start.Unix(), End: end.Unix()}
	if _, err := ReconcileAlertsStripeInvoice(ctx, app, person, reserved.Intent.ID, inv.ID, client); err != nil {
		t.Fatal(err)
	}
	grants, err = ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 0 {
		t.Fatal("refund replay restored access", grants, err)
	}
	client.ListInvoices = func(*stripe.InvoiceListParams) (*stripe.InvoiceList, error) {
		return &stripe.InvoiceList{Data: []*stripe.Invoice{inv}}, nil
	}
	beforeReads := invoiceReads
	if err = RecoverAlertsStripePayments(ctx, app, person, reserved.Intent.ID, client); err != nil {
		t.Fatal(err)
	}
	if invoiceReads != beforeReads {
		t.Fatal("polling revisited refunded invoice")
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET next_reconcile_at='1970-01-01' WHERE id=$1`, reserved.Intent.ID); err != nil {
		t.Fatal(err)
	}
	polled := false
	_, err = reconcileDueAlertsStripeCheckouts(ctx, app, func(_ context.Context, _ string, id string) error {
		if id != reserved.Intent.ID {
			return nil
		}
		polled = true
		var scheduled bool
		if err := app.DB.QueryRow(ctx, `SELECT next_reconcile_at>now()+interval '59 minutes' FROM alerts_stripe_checkouts WHERE id=$1`, id).Scan(&scheduled); err != nil || !scheduled {
			t.Fatal("paid subscription hourly recovery", scheduled, err)
		}
		return nil
	})
	if err != nil || !polled {
		t.Fatal("paid subscription excluded from recovery", polled, err)
	}
	var snapshot []byte
	if err := app.DB.QueryRow(ctx, `SELECT quote_snapshot FROM service_purchase_requests WHERE order_id=$1`, first.OrderID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var quote ServiceQuote
	if err := json.Unmarshal(snapshot, &quote); err != nil || quote.TaxCents != 36 || quote.TotalCents != 400 {
		t.Fatal("lost tax snapshot", quote, err)
	}
}
