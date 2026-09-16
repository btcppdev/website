package getters

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
)

func TestAlertsStripeIncreasePaymentPostgres(t *testing.T) {
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
	end := start.AddDate(0, 1, 0)
	b := alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, PaidSlots: 2}
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
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = recordAlertsStripeIncreasePayment(ctx, app, person, paid); err == nil {
		t.Fatal("unpaid base granted upgrade")
	}
	if _, err = recordAlertsStripeInvoice(app, b, alertsstripe.PaidPeriod{InvoiceID: "in_base_" + id, SubscriptionID: b.SubscriptionID, Quantity: 2, TotalCents: 400, TaxCents: 30, Start: start, End: end, PaidAt: start}); err != nil {
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
	var wg sync.WaitGroup
	results := make(chan *ServicePurchase, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := recordAlertsStripeIncreasePayment(ctx, app, person, paid)
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
}
