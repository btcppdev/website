package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"errors"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestBitcoinSwitchRefundedSubscriptionPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	for _, scenario := range []string{"canceled_refunded", "active_refunded", "cancelling_refunded", "canceled_paid", "canceled_partial", "unknown_invoice", "provider_failure", "retained_access", "cancelling_refunded_consent", "active_refunded_consent", "partial_consent", "cancel_timeout"} {
		t.Run(scenario, func(t *testing.T) {
			var person string
			if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Refunded checkout fixture') RETURNING id::text`).Scan(&person); err != nil {
				t.Fatal(err)
			}
			defer app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
			if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
				t.Fatal(err)
			}
			card, err := ReserveAlertsStripeCheckout(app, person, "refund-switch-"+person, 1, "https://btcpp.dev")
			if err != nil {
				t.Fatal(err)
			}
			price := &stripe.Price{ID: "price_" + card.Intent.ID, Currency: "usd", UnitAmount: 2400, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "year", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_" + card.Intent.ID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
			session := &stripe.CheckoutSession{ID: "cs_test_" + card.Intent.ID, Status: "open", Mode: "subscription", ExpiresAt: time.Now().Add(time.Hour).Unix(), ClientReferenceID: card.Intent.ID, Metadata: map[string]string{"product": "alerts.repository-slots", "account_id": person, "checkout_intent": card.Intent.ID}, Customer: &stripe.Customer{ID: "cus_" + person}, Subscription: &stripe.Subscription{ID: "sub_" + card.Intent.ID}, LineItems: &stripe.LineItemList{Data: []*stripe.LineItem{{Quantity: 1, Price: price}}}}
			session.URL = "https://checkout.stripe.com/c/pay/" + session.ID
			now := time.Now().UTC().Truncate(time.Second)
			start := now.Add(-time.Hour)
			end := start.AddDate(1, 0, 0)
			inv := &stripe.Invoice{ID: "in_" + card.Intent.ID, Status: "paid", Currency: "usd", Customer: session.Customer, BillingReason: "subscription_create", Total: 2400, AmountPaid: 2400, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: session.Subscription, Metadata: session.Metadata}}, TotalTaxes: []*stripe.InvoiceTotalTax{{Amount: 36, TaxBehavior: "inclusive"}}, Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{{Currency: "usd", Amount: 2400, Quantity: 1, Period: &stripe.Period{Start: start.Unix(), End: end.Unix()}, Parent: &stripe.InvoiceLineItemParent{Type: "subscription_item_details", SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: session.Subscription.ID}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: price.Product.ID}}}}}}
			client := alertsstripe.Client{Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Invoice: func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return inv, nil }, Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return price, nil }}

			session.Status = "complete"
			session.PaymentStatus = "paid"
			session.Invoice = &stripe.Invoice{ID: inv.ID}
			if _, err = ResolveAlertsStripeSubscription(ctx, app, person, card.Intent.ID, client, session.ID); err != nil {
				t.Fatal(err)
			}
			receipt, err := ReconcileAlertsStripeInvoice(ctx, app, person, card.Intent.ID, inv.ID, client)
			if err != nil {
				t.Fatal(err)
			}
			status := "refunded"
			if scenario == "canceled_paid" {
				status = "paid"
			}
			if scenario == "canceled_partial" || scenario == "partial_consent" {
				status = "partially_refunded"
			}
			if status != "paid" {
				var attempt string
				if err = app.DB.QueryRow(ctx, `SELECT a.id::text FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.order_id=$1`, receipt.OrderID).Scan(&attempt); err != nil {
					t.Fatal(err)
				}
				amount := int64(2400)
				if status == "partially_refunded" {
					amount = 1200
				}
				refund := VerifiedServiceRefund{AttemptID: attempt, Provider: "stripe", PaymentReference: inv.ID, RefundReference: "re_" + card.Intent.ID, AmountCents: amount, Currency: "USD", RefundedAt: now}
				if err = RecordServiceRefund(ctx, app, refund); err != nil {
					t.Fatal(err)
				}
				if scenario == "retained_access" {
					if _, err = app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
						t.Fatal(err)
					}
					if err = RecordServiceRefundAccessDecision(ctx, app, person, "stripe", refund.RefundReference, "retain", "Fixture retained access"); err != nil {
						t.Fatal(err)
					}
				}
			}

			sub := &stripe.Subscription{ID: session.Subscription.ID, Customer: session.Customer, Metadata: session.Metadata, Status: "canceled", LatestInvoice: session.Invoice, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Quantity: 1, Price: price, CurrentPeriodStart: start.Unix(), CurrentPeriodEnd: end.Unix()}}}}
			if scenario == "active_refunded" || scenario == "cancelling_refunded" || scenario == "cancelling_refunded_consent" || scenario == "active_refunded_consent" || scenario == "partial_consent" || scenario == "cancel_timeout" {
				sub.Status = "active"
			}
			if scenario == "cancelling_refunded" || scenario == "cancelling_refunded_consent" || scenario == "partial_consent" || scenario == "cancel_timeout" {
				sub.CancelAtPeriodEnd = true
			}
			if scenario == "unknown_invoice" {
				sub.LatestInvoice = &stripe.Invoice{ID: "in_unknown"}
			}
			client.Subscription = func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) {
				if scenario == "provider_failure" {
					return nil, errors.New("fixture unavailable")
				}
				return sub, nil
			}
			client.Expire = func(string, *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
				t.Fatal("attempted to expire a completed checkout")
				return nil, nil
			}
			cancellations := 0
			client.CancelSubscription = func(id string, p *stripe.SubscriptionCancelParams) (*stripe.Subscription, error) {
				cancellations++
				if id != sub.ID || p.InvoiceNow == nil || *p.InvoiceNow || p.Prorate == nil || *p.Prorate || p.IdempotencyKey == nil {
					t.Fatal("unsafe cancellation request")
				}
				var consents int
				if e := app.DB.QueryRow(ctx, `SELECT count(*) FROM alerts_refunded_card_end_consents WHERE checkout_id=$1 AND person_id=$2`, card.Intent.ID, person).Scan(&consents); e != nil || consents != 1 {
					t.Fatal("consent not durable before provider call", consents, e)
				}
				if scenario == "cancel_timeout" {
					return nil, errors.New("provider timeout")
				}
				sub.Status = "canceled"
				sub.CancelAtPeriodEnd = false
				return sub, nil
			}
			revision, err := AlertsBillingRevision(app, person)
			if err != nil {
				t.Fatal(err)
			}
			from := time.Now().UTC()
			to := from.AddDate(1, 0, 0)
			expiry := from.Add(10 * time.Minute)
			quote := ServiceQuote{AccountRevision: revision, BillingMonths: 12, ProductCode: "alerts.repository-slots", Name: "Alerts", Provider: "opennode", Quantity: 1, UnitCents: 2400, TotalCents: 2400, TaxCents: 200, PeriodStart: &from, PeriodEnd: &to, ExpiresAt: expiry}
			tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_switch_" + person, SalesTaxAmountCents: 200, DestinationCountry: "US", RawResponse: `{"amount_total":2400}`, ExpiresAt: &expiry}
			saved, err := SaveAlertsCheckoutQuote(app, person, quote, tax)
			if err != nil {
				t.Fatal(err)
			}
			consent := scenario == "cancelling_refunded_consent" || scenario == "active_refunded_consent" || scenario == "partial_consent" || scenario == "cancel_timeout"
			purchase, err := ConfirmServiceCheckoutQuoteWithCardEndConsent(ctx, app, person, saved.ID, "", consent, client)
			after, e := AlertsStripeCheckoutForPerson(app, person, card.Intent.ID)
			if e != nil {
				t.Fatal(e)
			}
			var count int
			if e = app.DB.QueryRow(ctx, `SELECT count(*) FROM service_purchase_requests WHERE person_id=$1 AND quote_snapshot->>'provider'='opennode'`, person).Scan(&count); e != nil {
				t.Fatal(e)
			}
			if scenario == "canceled_refunded" || scenario == "cancelling_refunded_consent" {
				if err != nil || purchase == nil || purchase.OrderID == receipt.OrderID || !after.Closed || count != 1 {
					t.Fatal(purchase, err, after.Closed, count)
				}
				again, e := ConfirmServiceCheckoutQuoteSwitchingToBitcoin(ctx, app, person, saved.ID, "", client)
				if e != nil || again.OrderID != purchase.OrderID {
					t.Fatal("non-idempotent retry", again, e)
				}
				var oldStatus string
				if e = app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, receipt.OrderID).Scan(&oldStatus); e != nil || oldStatus != "refunded" {
					t.Fatal("old receipt changed", oldStatus, e)
				}
			} else if !errors.Is(err, ErrAlertsStripeCheckoutConflict) || purchase != nil || after.Closed || count != 0 {
				t.Fatal("unsafe retirement", purchase, err, after.Closed, count)
			}
			wantCancels := 0
			if scenario == "cancelling_refunded_consent" || scenario == "cancel_timeout" {
				wantCancels = 1
			}
			if cancellations != wantCancels {
				t.Fatal("unexpected cancellation", cancellations, wantCancels)
			}
			if scenario == "cancelling_refunded" && !errors.Is(err, ErrRefundedCardEndConsent) {
				t.Fatal("missing explicit consent prompt", err)
			}

		})
	}
}
