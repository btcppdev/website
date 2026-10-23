package getters

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/servicebilling"
	"github.com/stripe/stripe-go/v86"
)

func TestAlertsStripeIncreasePaymentPostgres(t *testing.T) {
	for _, action := range []string{"none", "retain", "revoke", "unreviewed", "late-upgrade-revoke", "sales-closed"} {
		t.Run(action, func(t *testing.T) { testAlertsStripeIncreasePayment(t, action) })
	}
}

func testAlertsStripeIncreasePayment(t *testing.T, refundAction string) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Stripe paid increase') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	checkout, err := ReserveAlertsStripeCheckout(app, person, "paid-increase-test-key", 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	id := checkout.Intent.ID
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Hour)
	end := start.AddDate(1, 0, 0)
	b := alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, CustomerID: "cus_" + id, PriceID: "price_" + id, ProductID: "prod_" + id, PaidSlots: 2}
	raw, _ := json.Marshal(b)
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, id, b.SubscriptionID, raw); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(alertsstripe.SubscriptionState{State: "active", ProviderStatus: "active", PeriodStart: start.Unix(), PeriodEnd: end.Unix()})
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscription_state(checkout_id,revision,snapshot,observed_at) VALUES($1,1,$2,now())`, id, state); err != nil {
		t.Fatal(err)
	}
	q, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, alertsstripe.IncreasePreview{SubscriptionItemID: "si_test", PreviousSlots: 2, PaidSlots: 5, PeriodStart: start.Unix(), PeriodEnd: end.Unix(), ProrationDate: now.Unix(), TotalCents: 295, TaxCents: 25, ExpiresAt: now.Add(10 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	paid := alertsstripe.PaidIncrease{QuoteID: q.ID, InvoiceID: "in_increase_" + id, SubscriptionID: b.SubscriptionID, PreviousSlots: 2, PaidSlots: 5, AdditionalSlots: 3, TotalCents: 295, TaxCents: 25, Start: now, End: end, PaidAt: now}
	if _, err = recordAlertsStripeIncreasePayment(ctx, app, person, paid); err == nil {
		t.Fatal("unconfirmed quote fulfilled")
	}
	policy := ServiceLegalPolicy{ProductCode: servicebilling.AlertsProduct, Version: "upgrade-" + person, TermsText: "Upgrade agreement", PrivacyText: "Upgrade privacy", EffectiveAt: now.Add(-time.Hour)}
	if err := RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal(err)
	}
	defer app.DB.Exec(ctx, `DELETE FROM service_legal_current WHERE product_code=$1`, policy.ProductCode)
	if _, err = ReserveAlertsStripeIncreaseWithPolicy(ctx, app, person, q.ID, policy.Version); err != nil {
		t.Fatal(err)
	}
	if refundAction == "sales-closed" {
		if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
			t.Fatal(err)
		}
		if err := StopServiceSales(ctx, app, person, "alerts", "Permanent closure"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := app.DB.Exec(ctx, `TRUNCATE service_closure_plans,service_sales_closures`); err != nil {
				t.Error(err)
			}
		})
		if _, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, q.Preview); !errors.Is(err, ErrServiceSalesClosed) {
			t.Fatal("new upgrade quote", err)
		}
		if _, err := ReserveAlertsStripeIncreaseWithPolicy(ctx, app, person, q.ID, policy.Version); !errors.Is(err, ErrServiceSalesClosed) {
			t.Fatal("upgrade consent", err)
		}
		if _, err := PrepareAlertsStripeIncrease(ctx, app, person, id, 6, alertsstripe.Client{}); !errors.Is(err, ErrServiceSalesClosed) {
			t.Fatal("upgrade prepare", err)
		}
		if _, err := SubmitAlertsStripeIncrease(ctx, app, person, q.ID, alertsstripe.Client{}); !errors.Is(err, ErrServiceSalesClosed) {
			t.Fatal("upgrade submission", err)
		}
		if _, err := AlertsStripeIncreasePaymentLink(ctx, app, person, q.ID, alertsstripe.Client{}); !errors.Is(err, ErrServiceSalesClosed) {
			t.Fatal("upgrade payment link", err)
		}
	}
	if _, err = recordAlertsStripeIncreasePayment(ctx, app, person, paid); err == nil {
		t.Fatal("unpaid base granted upgrade")
	}
	if _, err = recordAlertsStripeInvoice(app, b, alertsstripe.PaidPeriod{InvoiceID: "in_base_" + id, SubscriptionID: b.SubscriptionID, Quantity: 2, TotalCents: 4800, TaxCents: 30, Start: start, End: end, PaidAt: start}); err != nil {
		t.Fatal(err)
	}
	next := policy
	next.Version += "-next"
	next.TermsText = "Replacement terms"
	next.EffectiveAt = now.Add(-time.Minute)
	if err := RegisterServiceLegalPolicy(app, next); err != nil {
		t.Fatal(err)
	}
	if err := ActivateServiceLegalPolicy(app, next.ProductCode, next.Version); err != nil {
		t.Fatal(err)
	}
	// Force an error late in the transaction. No receipt or grant may leak.
	collision := paid
	collision.InvoiceID = "in_base_" + id
	if _, err = recordAlertsStripeIncreasePayment(ctx, app, person, collision); err == nil {
		t.Fatal("duplicate provider invoice accepted")
	}
	grants, err := ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 2 {
		t.Fatal("failed transaction leaked grant", grants, err)
	}
	upgradePrice := &stripe.Price{ID: b.PriceID, Currency: "usd", UnitAmount: 2400, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "year", IntervalCount: 1}, Product: &stripe.Product{ID: b.ProductID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	upgradeInvoice := &stripe.Invoice{ID: paid.InvoiceID, Status: "paid", Currency: "usd", Customer: &stripe.Customer{ID: b.CustomerID}, BillingReason: "subscription_update", Total: 295, AmountDue: 295, AmountPaid: 295, StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, TotalTaxes: []*stripe.InvoiceTotalTax{{Amount: 25, TaxBehavior: "inclusive"}}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: &stripe.Subscription{ID: b.SubscriptionID}, Metadata: map[string]string{"account_id": person, "checkout_intent": id, "product": "alerts.repository-slots"}}}, Lines: &stripe.InvoiceLineItemList{}}
	for _, line := range []struct{ quantity, amount int64 }{{2, -200}, {5, 495}} {
		upgradeInvoice.Lines.Data = append(upgradeInvoice.Lines.Data, &stripe.InvoiceLineItem{Currency: "usd", Quantity: line.quantity, Amount: line.amount, Period: &stripe.Period{Start: now.Unix(), End: end.Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: b.SubscriptionID, SubscriptionItem: "si_test", Proration: true}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: upgradePrice, Product: b.ProductID}}})
	}
	upgradeClient := alertsstripe.Client{Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return upgradePrice, nil }, Invoice: func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return upgradeInvoice, nil }, ListInvoices: func(*stripe.InvoiceListParams) (*stripe.InvoiceList, error) {
		return &stripe.InvoiceList{Data: []*stripe.Invoice{upgradeInvoice}}, nil
	}}
	upgradeInvoice.Status = "open"
	if _, err = ReconcileAlertsStripeIncreaseInvoice(ctx, app, person, q.ID, paid.InvoiceID, upgradeClient); err == nil {
		t.Fatal("unpaid provider invoice granted access")
	}
	upgradeInvoice.Status = "paid"
	// Simulate a committed attempt whose response/reference was lost while
	// Stripe left the upgrade awaiting payment.
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increase_submissions(quote_id,lease_until) VALUES($1,now()-interval '1 second')`, q.ID); err != nil {
		t.Fatal(err)
	}
	upgradeInvoice.Status = "open"
	upgradeInvoice.AmountPaid = 0
	upgradeInvoice.AmountRemaining = 295
	upgradeInvoice.StatusTransitions = nil
	upgradeInvoice.HostedInvoiceURL = "https://invoice.stripe.com/i/acct_test/upgrade"
	upgradeClient.Subscription = func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) {
		return &stripe.Subscription{ID: b.SubscriptionID, Customer: upgradeInvoice.Customer, Metadata: upgradeInvoice.Parent.SubscriptionDetails.Metadata, Status: "active", LatestInvoice: &stripe.Invoice{ID: paid.InvoiceID}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{ID: "si_test", Quantity: 2, Price: upgradePrice, CurrentPeriodStart: start.Unix(), CurrentPeriodEnd: end.Unix()}}}}, nil
	}
	if observed, e := RecoverAlertsStripeIncreaseReference(ctx, app, person, q.ID, upgradeClient); e != nil || observed == nil || observed.Status != "open" {
		t.Fatal("unpaid reference recovery", observed, e)
	}
	link, e := AlertsStripeIncreasePaymentLink(ctx, app, person, q.ID, upgradeClient)
	if refundAction == "sales-closed" {
		if link != "" || !errors.Is(e, ErrServiceSalesClosed) {
			t.Fatal("closed sales payment handoff", link, e)
		}
	} else if e != nil || link != upgradeInvoice.HostedInvoiceURL {
		t.Fatal("account payment handoff", link, e)
	}
	if _, e := AlertsStripeIncreasePaymentLink(ctx, app, "00000000-0000-0000-0000-000000000000", q.ID, upgradeClient); e == nil {
		t.Fatal("foreign payment handoff")
	}
	recovered, e := RecoverAlertsStripeIncrease(ctx, app, person, q.ID, upgradeClient)
	if refundAction == "sales-closed" {
		// This fixture has no void adapter or verified pending update. Closure
		// must remain unresolved; a later successful payment is still recorded.
		if recovered || !errors.Is(e, alertsstripe.ErrReconcile) {
			t.Fatal("unverified closure accepted", recovered, e)
		}
	} else if e != nil || recovered {
		t.Fatal("unpaid invoice fulfilled", recovered, e)
	}
	upgradeInvoice.Status = "paid"
	upgradeInvoice.AmountPaid = 295
	upgradeInvoice.AmountRemaining = 0
	upgradeInvoice.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}
	if _, err = ReconcileAlertsStripeIncreaseInvoice(ctx, app, "00000000-0000-0000-0000-000000000000", q.ID, paid.InvoiceID, upgradeClient); err == nil {
		t.Fatal("foreign account reconciled upgrade")
	}
	if recovered, e := RecoverAlertsStripeIncrease(ctx, app, person, q.ID, upgradeClient); e != nil || !recovered {
		t.Fatal("lost invoice reference recovery", recovered, e)
	}
	var wg sync.WaitGroup
	results := make(chan *ServicePurchase, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := ReconcileAlertsStripeIncreaseInvoice(ctx, app, person, q.ID, paid.InvoiceID, upgradeClient)
			if e != nil {
				t.Error(e)
			}
			results <- out
		}()
	}
	wg.Wait()
	close(results)
	var order string
	for out := range results {
		if out == nil {
			t.Fatal("missing receipt")
		}
		if order != "" && order != out.OrderID {
			t.Fatal("duplicate receipt")
		}
		order = out.OrderID
	}
	grants, err = ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 2 {
		t.Fatal(grants, err)
	}
	var quantity int64
	for _, g := range grants {
		quantity += g.Quantity
		if !g.EndsAt.Equal(end) {
			t.Fatal("extended billing period", g)
		}
	}
	if quantity != 5 {
		t.Fatal("wrong total allowance", quantity)
	}
	var receiptRaw, acceptanceRaw []byte
	if err := app.DB.QueryRow(ctx, `SELECT r.quote_snapshot,i.legal_acceptance FROM service_purchase_requests r JOIN alerts_stripe_increase_payments p ON p.order_id=r.order_id JOIN alerts_stripe_increases i ON i.quote_id=p.quote_id WHERE r.order_id=$1`, order).Scan(&receiptRaw, &acceptanceRaw); err != nil {
		t.Fatal(err)
	}
	var receipt ServiceQuote
	var accepted servicebilling.LegalAcceptance
	if err := json.Unmarshal(receiptRaw, &receipt); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(acceptanceRaw, &accepted); err != nil {
		t.Fatal(err)
	}
	if receipt.LegalAcceptance == nil || *receipt.LegalAcceptance != accepted || accepted.Version != policy.Version {
		t.Fatal("receipt changed upgrade agreement", receipt.LegalAcceptance)
	}
	var total, tax, count int64
	var consistent bool
	if err = app.DB.QueryRow(ctx, `SELECT sum(line_total_cents),sum(tax_amount_cents),sum(quantity),bool_and(line_total_cents=quantity*unit_price_cents) FROM shop_order_items WHERE order_id=$1`, order).Scan(&total, &tax, &count, &consistent); err != nil || total != 295 || tax != 25 || count != 3 || !consistent {
		t.Fatal("receipt rounding", total, tax, count, consistent, err)
	}
	var status string
	var slots int64
	if err = app.DB.QueryRow(ctx, `SELECT i.state,(p.binding->>'PaidSlots')::bigint FROM alerts_stripe_increases i JOIN alerts_stripe_increase_payments p USING(quote_id) WHERE quote_id=$1`, q.ID).Scan(&status, &slots); err != nil || status != "applied" || slots != 5 {
		t.Fatal(status, slots, err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_payments SET binding='{}' WHERE quote_id=$1`, q.ID); err == nil {
		t.Fatal("mutable payment evidence")
	}
	price := &stripe.Price{ID: b.PriceID, Currency: "usd", UnitAmount: 2400, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "year", IntervalCount: 1}, Product: &stripe.Product{ID: b.ProductID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	meta := map[string]string{"account_id": person, "checkout_intent": id, "product": "alerts.repository-slots"}
	sub := &stripe.Subscription{ID: b.SubscriptionID, Customer: &stripe.Customer{ID: b.CustomerID}, Metadata: meta, Status: "active", Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{ID: "si_test", Quantity: 5, Price: price, CurrentPeriodStart: start.Unix(), CurrentPeriodEnd: end.Unix()}}}}
	client := alertsstripe.Client{Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }, Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return price, nil }}
	if err = RefreshAlertsStripeSubscription(ctx, app, person, id, client); err != nil {
		t.Fatal("refresh after upgrade", err)
	}
	nextEnd := end.AddDate(1, 0, 0)
	inv := &stripe.Invoice{ID: "in_renewal_" + id, Status: "open", Currency: "usd", Customer: sub.Customer, BillingReason: "subscription_cycle", Total: 12000, AmountRemaining: 12000, Attempted: true, AttemptCount: 1, CollectionMethod: "charge_automatically", AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: &stripe.Subscription{ID: b.SubscriptionID}, Metadata: meta}}, Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{{Currency: "usd", Amount: 12000, Quantity: 5, Period: &stripe.Period{Start: end.Unix(), End: nextEnd.Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: b.SubscriptionID}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: b.ProductID}}}}}}
	client.Invoice = func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return inv, nil }
	sub.Status = "past_due"
	sub.LatestInvoice = &stripe.Invoice{ID: inv.ID}
	sub.Items.Data[0].CurrentPeriodStart = end.Unix()
	sub.Items.Data[0].CurrentPeriodEnd = nextEnd.Unix()

	if refundAction != "none" && refundAction != "late-upgrade-revoke" && refundAction != "sales-closed" {
		if _, err = app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
			t.Fatal(err)
		}
		for _, invoice := range []string{"in_base_" + id, paid.InvoiceID} {
			var attempt, reference string
			if err = app.DB.QueryRow(ctx, `SELECT a.id::text,a.provider_reference FROM service_payment_attempts a WHERE a.provider='stripe' AND a.provider_reference=$1`, invoice).Scan(&attempt, &reference); err != nil {
				t.Fatal(err)
			}
			refund := VerifiedServiceRefund{AttemptID: attempt, Provider: "stripe", PaymentReference: reference, RefundReference: "re_" + attempt, AmountCents: 1, Currency: "USD", RefundedAt: now.Add(time.Second)}
			if err = RecordServiceRefund(ctx, app, refund); err != nil {
				t.Fatal(err)
			}
			if refundAction != "unreviewed" {
				if err = RecordServiceRefundAccessDecision(ctx, app, person, "stripe", refund.RefundReference, refundAction, "Exceptional refund grace fixture"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err = refreshAlertsStripeSubscriptionAt(ctx, app, person, id, client, end.Add(time.Hour)); err != nil {
		t.Fatal("failed upgraded renewal", err)
	}
	if refundAction == "revoke" || refundAction == "unreviewed" {
		var rows int
		if err = app.DB.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_renewals WHERE invoice_id=$1 AND state='failed'`, inv.ID).Scan(&rows); err != nil || rows != 0 {
			t.Fatal("revoked payment generated grace", rows, err)
		}
	} else {
		if err = app.DB.QueryRow(ctx, `SELECT paid_slots FROM alerts_stripe_renewals WHERE invoice_id=$1`, inv.ID).Scan(&slots); err != nil || slots != 5 {
			t.Fatal("upgrade missing from grace", slots, err)
		}
	}

	if refundAction == "late-upgrade-revoke" {
		if _, err = app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
			t.Fatal(err)
		}
		var attempt string
		if err = app.DB.QueryRow(ctx, `SELECT id::text FROM service_payment_attempts WHERE provider='stripe' AND provider_reference=$1`, paid.InvoiceID).Scan(&attempt); err != nil {
			t.Fatal(err)
		}
		refund := VerifiedServiceRefund{AttemptID: attempt, Provider: "stripe", PaymentReference: paid.InvoiceID, RefundReference: "re_late_" + attempt, AmountCents: 1, Currency: "USD", RefundedAt: end.Add(2 * time.Hour)}
		if err = RecordServiceRefund(ctx, app, refund); err != nil {
			t.Fatal(err)
		}
		if err = RecordServiceRefundAccessDecision(ctx, app, person, "stripe", refund.RefundReference, "revoke", "Revoke upgrade after grace started"); err != nil {
			t.Fatal(err)
		}
		remaining, e := ActiveServiceEntitlements(app, person, now)
		if e != nil || len(remaining) != 1 || remaining[0].Quantity != 2 || !remaining[0].EndsAt.Equal(end) {
			t.Fatal("upgrade refund changed unrelated base entitlement", remaining, e)
		}
		for _, delay := range []time.Duration{3 * time.Hour, 4 * time.Hour} {
			if err = refreshAlertsStripeSubscriptionAt(ctx, app, person, id, client, end.Add(delay)); err != nil {
				t.Fatal(err)
			}
		}
		var lifecycle string
		var revision int64
		var due time.Time
		if err = app.DB.QueryRow(ctx, `SELECT state,revision,due_at,paid_slots FROM alerts_stripe_renewals WHERE invoice_id=$1`, inv.ID).Scan(&lifecycle, &revision, &due, &slots); err != nil || lifecycle != "expired" || revision != 2 || !due.Equal(end) || slots != 5 {
			t.Fatal("stale grace or altered evidence", lifecycle, revision, due, slots, err)
		}
		snapshot, err := AccountAccess(ctx, app, person, end.Add(4*time.Hour))
		if err != nil || len(snapshot.Renewals) != 1 || snapshot.Renewals[0].State != "expired" {
			t.Fatal("access API retained stale grace", snapshot, err)
		}
	}
	inv.Status = "paid"
	inv.AmountPaid = 12000
	inv.AmountRemaining = 0
	inv.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: end.Add(time.Hour).Unix()}
	if _, err = ReconcileAlertsStripeInvoice(ctx, app, person, id, inv.ID, client); err != nil {
		t.Fatal("renewal after upgrade", err)
	}
	grants, err = ActiveServiceEntitlements(app, person, end.Add(2*time.Hour))
	if err != nil || len(grants) != 1 || grants[0].Quantity != 5 {
		t.Fatal("renewal allowance", grants, err)
	}
	// Replay an original pre-upgrade invoice through the authenticated lookup.
	inv.ID = "in_base_" + id
	inv.Total = 4800
	inv.AmountPaid = 4800
	inv.BillingReason = "subscription_create"
	inv.StatusTransitions.PaidAt = start.Unix()
	inv.TotalTaxes = []*stripe.InvoiceTotalTax{{Amount: 30, TaxBehavior: "inclusive"}}
	inv.Lines.Data[0].Amount = 4800
	inv.Lines.Data[0].Quantity = 2
	inv.Lines.Data[0].Period = &stripe.Period{Start: start.Unix(), End: end.Unix()}
	if _, err = ReconcileAlertsStripeInvoice(ctx, app, person, id, inv.ID, client); err != nil {
		t.Fatal("old invoice replay after upgrade", err)
	}
}
