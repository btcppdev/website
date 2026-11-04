package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/servicebilling"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestAlertsFiftyCheckoutPaymentCancellationRefundPostgres(t *testing.T) {
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
	policy := ServiceLegalPolicy{ProductCode: servicebilling.AlertsProduct, Version: uuid.NewString(), TermsText: "Subscription terms", PrivacyText: "Subscription privacy", EffectiveAt: time.Now().Add(-time.Hour)}
	if err := RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal(err)
	}
	defer app.DB.Exec(ctx, `DELETE FROM service_legal_current WHERE product_code=$1`, policy.ProductCode)
	reserved, err := ReserveAlertsStripePlan(app, person, "fifty-activation-key", 1, "https://btcpp.dev", policy.Version, servicebilling.AlertsFiftyProduct)
	if err != nil {
		t.Fatal(err)
	}
	next := policy
	next.Version = uuid.NewString()
	next.TermsText = "Replacement policy"
	next.EffectiveAt = time.Now().Add(-time.Minute)
	if err := RegisterServiceLegalPolicy(app, next); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, next.ProductCode, next.Version); err != nil {
		t.Fatal(err)
	}
	price := &stripe.Price{ID: "price_" + reserved.Intent.ID, Currency: "usd", UnitAmount: 18000, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "year", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_" + reserved.Intent.ID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	session := &stripe.CheckoutSession{ID: "cs_test_" + reserved.Intent.ID, Status: "open", Mode: "subscription", ExpiresAt: time.Now().Add(time.Hour).Unix(), ClientReferenceID: reserved.Intent.ID, Metadata: map[string]string{"product": servicebilling.AlertsFiftyProduct, "account_id": person, "checkout_intent": reserved.Intent.ID}, Customer: &stripe.Customer{ID: "cus_" + person}, Subscription: &stripe.Subscription{ID: "sub_" + reserved.Intent.ID}, LineItems: &stripe.LineItemList{Data: []*stripe.LineItem{{Quantity: 1, Price: price}}}}
	session.URL = "https://checkout.stripe.com/c/pay/" + session.ID
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Hour)
	end := start.AddDate(1, 0, 0)
	inv := &stripe.Invoice{ID: "in_" + reserved.Intent.ID, Status: "paid", Currency: "usd", Customer: session.Customer, BillingReason: "subscription_create", Total: 18000, AmountPaid: 18000, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: session.Subscription, Metadata: session.Metadata}}, TotalTaxes: []*stripe.InvoiceTotalTax{{Amount: 36, TaxBehavior: "inclusive"}}, Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{{Currency: "usd", Amount: 18000, Quantity: 1, Period: &stripe.Period{Start: start.Unix(), End: end.Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: session.Subscription.ID}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: price.Product.ID}}}}}}
	client := alertsstripe.Client{Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Invoice: func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return inv, nil }, Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return price, nil }}
	// Simulate a lost creation response: the session exists at Stripe but its
	// reference was never saved locally. A verified completion can recover it.
	// The generated product family shares the account's Alerts purchase lock.
	if _, err := ReserveAlertsStripeCheckoutWithPolicy(app, person, "different-plan-key", 1, "https://btcpp.dev", policy.Version); !errors.Is(err, ErrAlertsStripeCheckoutConflict) {
		t.Fatal("parallel per-project checkout accepted", err)
	}
	client.Create = func(p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		item := p.LineItems[0]
		if *item.Quantity != 1 || *item.PriceData.UnitAmount != 18000 || *item.PriceData.Recurring.Interval != "year" {
			t.Fatal("wrong annual checkout", item)
		}
		return session, nil
	}
	if _, err = OpenAlertsStripeCheckout(ctx, app, person, reserved.Intent.ID, client); err != nil {
		t.Fatal(err)
	}
	session.Status = "complete"
	binding, err := ResolveAlertsStripeSubscription(ctx, app, person, reserved.Intent.ID, client)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Product() != servicebilling.AlertsFiftyProduct {
		t.Fatal("lost plan identity", binding)
	}
	order, err := ReconcileAlertsStripeInvoice(ctx, app, person, reserved.Intent.ID, inv.ID, client)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ReconcileAlertsStripeInvoice(ctx, app, person, reserved.Intent.ID, inv.ID, client)
	if err != nil || again.OrderID != order.OrderID {
		t.Fatal("invoice replay duplicated payment", again, err)
	}
	access, err := AccountAccess(ctx, app, person, now)
	if err != nil || len(access.Entitlements) != 1 || access.Entitlements[0].ProductCode != servicebilling.AlertsFiftyProduct || access.Entitlements[0].Quantity != 1 {
		t.Fatal("wrong entitlement", access, err)
	}
	var quote ServiceQuote
	var raw []byte
	if err = app.DB.QueryRow(ctx, `SELECT quote_snapshot FROM service_purchase_requests WHERE order_id=$1`, order.OrderID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &quote); err != nil || quote.BillingMonths != 12 || quote.TotalCents != 18000 || quote.UnitCents != 18000 {
		t.Fatal("wrong receipt", quote, err)
	}
	sub := &stripe.Subscription{ID: binding.SubscriptionID, Status: "active", Customer: session.Customer, Metadata: session.Metadata, CollectionMethod: "charge_automatically", Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{ID: "si_fifty", Quantity: 1, Price: price, CurrentPeriodStart: start.Unix(), CurrentPeriodEnd: end.Unix()}}}}
	client.Subscription = func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }
	client.UpdateSubscription = func(_ string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
		if p.CancelAtPeriodEnd == nil || !*p.CancelAtPeriodEnd {
			t.Fatal("unexpected subscription mutation")
		}
		sub.CancelAtPeriodEnd = true
		return sub, nil
	}
	if err = RefreshAlertsStripeSubscription(ctx, app, person, reserved.Intent.ID, client); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err = app.DB.QueryRow(ctx, `SELECT revision FROM alerts_stripe_subscription_state WHERE checkout_id=$1`, reserved.Intent.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := StartAlertsBitcoinSwitch(ctx, app, person, reserved.Intent.ID, uuid.NewString(), revision, end.Unix(), client); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("bundle interpreted as per-project Bitcoin switch", err)
	}
	cancel, err := ReserveAlertsStripeCancellation(ctx, app, person, reserved.Intent.ID, uuid.NewString(), revision, end.Unix())
	if err != nil {
		t.Fatal(err)
	}
	status, err := ProcessAlertsStripeCancellation(ctx, app, person, cancel.Intent.ID, client)
	if err != nil || status != "confirmed" {
		t.Fatal("cancellation failed", status, err)
	}
	access, err = AccountAccess(ctx, app, person, now)
	if err != nil || len(access.Entitlements) != 1 {
		t.Fatal("cancellation removed paid access", access, err)
	}
	if _, err = client.PreviewIncrease(ctx, *binding, 2, now); err == nil {
		t.Fatal("increased fixed bundle quantity")
	}
	var attempt string
	if err = app.DB.QueryRow(ctx, `SELECT a.id::text FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.order_id=$1`, order.OrderID).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	if err = RecordServiceRefund(ctx, app, VerifiedServiceRefund{AttemptID: attempt, Provider: "stripe", PaymentReference: inv.ID, RefundReference: "re_" + reserved.Intent.ID, AmountCents: 18000, Currency: "USD", RefundedAt: now.Add(time.Second), Reason: "test refund"}); err != nil {
		t.Fatal(err)
	}
	access, err = AccountAccess(ctx, app, person, now.Add(2*time.Second))
	if err != nil || len(access.Entitlements) != 0 {
		t.Fatal("refunded plan retained access", access, err)
	}
}
