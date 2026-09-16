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

func TestAlertsStripeIncreaseReservationPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Stripe increase reservation') RETURNING id::text`).Scan(&person); err != nil {
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
	checkout, err := ReserveAlertsStripeCheckout(app, person, "increase-reservation", 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	id := checkout.Intent.ID
	now := time.Now().UTC().Truncate(time.Second)
	preview := alertsstripe.IncreasePreview{SubscriptionItemID: "si_test", PreviousSlots: 2, PaidSlots: 3, PeriodStart: now.Add(-time.Hour).Unix(), PeriodEnd: now.AddDate(0, 1, 0).Unix(), ProrationDate: now.Unix(), TotalCents: 195, TaxCents: 15, ExpiresAt: now.Add(10 * time.Minute)}
	b, _ := json.Marshal(alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, CustomerID: "cus_" + id, PriceID: "price_" + id, ProductID: "prod_" + id, PaidSlots: 2})
	s, _ := json.Marshal(alertsstripe.SubscriptionState{State: "active", ProviderStatus: "active", PeriodStart: preview.PeriodStart, PeriodEnd: preview.PeriodEnd})
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, id, "sub_"+id, b); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscription_state(checkout_id,revision,snapshot,observed_at) VALUES($1,1,$2,now())`, id, s); err != nil {
		t.Fatal(err)
	}
	quote, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, preview)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, preview)
	if err != nil {
		t.Fatal(err)
	}
	expired := preview
	expired.ExpiresAt = now.Add(-time.Second)
	if _, err = SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, expired); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("expired preview saved", err)
	}
	// Seed a quote that was valid when issued, but expired before confirmation.
	expired.ProrationDate = now.Add(-11 * time.Minute).Unix()
	expiredRaw, _ := json.Marshal(expired)
	var expiredID string
	if err = app.DB.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_quotes(checkout_id,revision,preview,created_at,expires_at) VALUES($1,1,$2,$3,$4) RETURNING id::text`, id, expiredRaw, now.Add(-11*time.Minute), expired.ExpiresAt).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, expiredID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("expired quote confirmed", err)
	}
	if _, err = AlertsStripeIncreaseQuoteForPerson(ctx, app, uuid.NewString(), quote.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign quote readable", err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, uuid.NewString(), quote.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign quote confirmable", err)
	}
	if _, err = SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 2, preview); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale revision", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_quotes SET preview='{}' WHERE id=$1`, quote.ID); err == nil {
		t.Fatal("quote mutable")
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, e := ReserveAlertsStripeIncrease(ctx, app, person, quote.ID)
			if e != nil || status != "pending" {
				t.Errorf("reservation %s %v", status, e)
			}
		}()
	}
	wg.Wait()
	if resumed, e := PrepareAlertsStripeIncrease(ctx, app, person, id, 4, alertsstripe.Client{}); e != nil || resumed == nil || resumed.ID != quote.ID || resumed.Preview.PaidSlots != 3 {
		t.Fatal("pending upgrade replaced by new cart", resumed, e)
	}
	claims := make(chan *alertsIncreaseSubmissionClaim, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, e := claimAlertsStripeIncreaseSubmission(ctx, app, person, quote.ID)
			if e != nil && !errors.Is(e, alertsstripe.ErrReconcile) {
				t.Error(e)
			}
			claims <- claim
		}()
	}
	wg.Wait()
	close(claims)
	winners := 0
	for claim := range claims {
		if claim != nil {
			winners++
			if claim.Attempt != 1 || claim.Intent.ID != quote.ID || claim.Binding.PaidSlots != 2 {
				t.Fatal(claim)
			}
		}
	}
	if winners != 1 {
		t.Fatal("concurrent submission claims", winners)
	}
	if _, err = claimAlertsStripeIncreaseSubmission(ctx, app, uuid.NewString(), quote.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign submission claim", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET lease_until=now()-interval '1 second' WHERE quote_id=$1`, quote.ID); err != nil {
		t.Fatal(err)
	}
	retry, e := claimAlertsStripeIncreaseSubmission(ctx, app, person, quote.ID)
	if e != nil || retry == nil || retry.Attempt != 2 || retry.Intent.ID != quote.ID {
		t.Fatal("stable retry", retry, e)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET invoice_id=$2 WHERE quote_id=$1`, quote.ID, "in_claim_"+quote.ID); err != nil {
		t.Fatal(err)
	}
	if invoice, e := SubmitAlertsStripeIncrease(ctx, app, person, quote.ID, alertsstripe.Client{}); e != nil || invoice != "in_claim_"+quote.ID {
		t.Fatal("saved reference retry used provider", invoice, e)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET invoice_id='in_other' WHERE quote_id=$1`, quote.ID); err == nil {
		t.Fatal("invoice reference mutable")
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("overlapping upgrade", err)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, id, uuid.NewString(), 1, preview.PeriodEnd); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("cancellation during upgrade", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increases SET state='expired' WHERE quote_id=$1`, quote.ID); err != nil {
		t.Fatal(err)
	}
	if status, e := ReserveAlertsStripeIncrease(ctx, app, person, quote.ID); e != nil || status != "expired" {
		t.Fatal("resolved retry", status, e)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increases SET state='pending' WHERE quote_id=$1`, quote.ID); err == nil {
		t.Fatal("resolved instruction reopened")
	}
	// An expired quote with no submission can be released without provider I/O.
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increases(quote_id,checkout_id) VALUES($1,$2)`, expiredID, id); err != nil {
		t.Fatal(err)
	}
	if _, err = claimAlertsStripeIncreaseSubmission(ctx, app, person, expiredID); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("expired submission claimed", err)
	}
	var expiredState string
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, expiredID).Scan(&expiredState); err != nil || expiredState != "expired" {
		t.Fatal(expiredState, err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=2 WHERE checkout_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale confirmation", err)
	}
	fresh, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 2, preview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, id, uuid.NewString(), 2, preview.PeriodEnd); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, fresh.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("upgrade during cancellation", err)
	}
	grants, err := ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 0 {
		t.Fatal("reservation granted access", grants, err)
	}
	// Expiry cannot release an ambiguous attempted operation. Seed persisted
	// crash evidence directly to exercise recovery after the review window.
	var ambiguousID string
	if err = app.DB.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_quotes(checkout_id,revision,preview,created_at,expires_at) VALUES($1,2,$2,$3,$4) RETURNING id::text`, id, expiredRaw, now.Add(-11*time.Minute), expired.ExpiresAt).Scan(&ambiguousID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increases(quote_id,checkout_id) VALUES($1,$2)`, ambiguousID, id); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_increase_submissions(quote_id,lease_until) VALUES($1,now()-interval '1 second')`, ambiguousID); err != nil {
		t.Fatal(err)
	}
	if _, err = claimAlertsStripeIncreaseSubmission(ctx, app, person, ambiguousID); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("expired attempted claim", err)
	}
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, ambiguousID).Scan(&expiredState); err != nil || expiredState != "pending" {
		t.Fatal("ambiguous operation released", expiredState, err)
	}
	price := &stripe.Price{ID: "price_" + id, Currency: "usd", UnitAmount: 200, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "month", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_" + id, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	meta := map[string]string{"account_id": person, "checkout_intent": id, "product": "alerts.repository-slots"}
	invoice := &stripe.Invoice{ID: "in_void_" + ambiguousID, Status: "void", Currency: "usd", Customer: &stripe.Customer{ID: "cus_" + id}, BillingReason: "subscription_update", Total: 195, AmountDue: 195, AutomaticTax: &stripe.InvoiceAutomaticTax{Enabled: true, Status: "complete"}, TotalTaxes: []*stripe.InvoiceTotalTax{{Amount: 15, TaxBehavior: "inclusive"}}, Parent: &stripe.InvoiceParent{Type: "subscription_details", SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Subscription: &stripe.Subscription{ID: "sub_" + id}, Metadata: meta}}, Lines: &stripe.InvoiceLineItemList{}}
	for _, line := range []struct{ quantity, amount int64 }{{2, -200}, {3, 395}} {
		invoice.Lines.Data = append(invoice.Lines.Data, &stripe.InvoiceLineItem{Currency: "usd", Quantity: line.quantity, Amount: line.amount, Period: &stripe.Period{Start: expired.ProrationDate, End: expired.PeriodEnd}, Parent: &stripe.InvoiceLineItemParent{SubscriptionItemDetails: &stripe.InvoiceLineItemParentSubscriptionItemDetails{Subscription: "sub_" + id, SubscriptionItem: "si_test", Proration: true}}, Pricing: &stripe.InvoiceLineItemPricing{PriceDetails: &stripe.InvoiceLineItemPricingPriceDetails{Price: price, Product: price.Product.ID}}})
	}
	sub := &stripe.Subscription{ID: "sub_" + id, Customer: invoice.Customer, Metadata: meta, Status: "active", Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{ID: "si_test", Quantity: 2, Price: price, CurrentPeriodStart: expired.PeriodStart, CurrentPeriodEnd: expired.PeriodEnd}}}}
	client := alertsstripe.Client{Invoice: func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return invoice, nil }, Price: func(string, *stripe.PriceParams) (*stripe.Price, error) { return price, nil }, Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET invoice_id=$2,lease_until=now()+interval '1 minute' WHERE quote_id=$1`, ambiguousID, invoice.ID); err != nil {
		t.Fatal(err)
	}
	if err = ResolveVoidedAlertsStripeIncrease(ctx, app, person, ambiguousID, client); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("released live submission", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET lease_until=now()-interval '1 second' WHERE quote_id=$1`, ambiguousID); err != nil {
		t.Fatal(err)
	}
	sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{}
	if err = ResolveVoidedAlertsStripeIncrease(ctx, app, person, ambiguousID, client); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("released pending provider update", err)
	}
	sub.PendingUpdate = nil
	for i := 0; i < 2; i++ {
		if err = ResolveVoidedAlertsStripeIncrease(ctx, app, person, ambiguousID, client); err != nil {
			t.Fatal(err)
		}
	}
	if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, ambiguousID).Scan(&expiredState); err != nil || expiredState != "expired" {
		t.Fatal(expiredState, err)
	}
	if _, err = app.DB.Exec(ctx, `DELETE FROM alerts_stripe_increase_expirations WHERE quote_id=$1`, ambiguousID); err == nil {
		t.Fatal("expiry evidence deleted")
	}
}
