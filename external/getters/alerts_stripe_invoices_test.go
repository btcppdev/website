package getters

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
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
	if _, err := ReconcileAlertsStripeInvoice(ctx, app, "00000000-0000-0000-0000-000000000000", reserved.Intent.ID, inv.ID, client); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("cross-account invoice", err)
	}
	// A later renewal is a new purchase, while the previous interval remains intact.
	inv.ID += "_renewal"
	inv.BillingReason = "subscription_cycle"
	inv.Lines.Data[0].Period = &stripe.Period{Start: end.Unix(), End: end.AddDate(0, 1, 0).Unix()}
	if _, err := ReconcileAlertsStripeInvoice(ctx, app, person, reserved.Intent.ID, inv.ID, client); err != nil {
		t.Fatal(err)
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
	var snapshot []byte
	if err := app.DB.QueryRow(ctx, `SELECT quote_snapshot FROM service_purchase_requests WHERE order_id=$1`, first.OrderID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var quote ServiceQuote
	if err := json.Unmarshal(snapshot, &quote); err != nil || quote.TaxCents != 36 || quote.TotalCents != 400 {
		t.Fatal("lost tax snapshot", quote, err)
	}
}
