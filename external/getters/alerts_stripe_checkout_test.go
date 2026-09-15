package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
)

func TestAlertsStripeCheckoutPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Stripe checkout test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "stripe-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	const n = 5
	results := make(chan *AlertsStripeCheckout, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := ReserveAlertsStripeCheckout(app, person, "stripe-checkout-test-001", 2, "https://btcpp.dev")
			results <- out
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
	var first *AlertsStripeCheckout
	for out := range results {
		if first != nil && first.Intent.ID != out.Intent.ID {
			t.Fatal("duplicate checkout")
		}
		first = out
	}
	if _, err := ReserveAlertsStripeCheckout(app, person, "another-browser-tab-key", 3, "https://btcpp.dev"); !errors.Is(err, ErrAlertsStripeCheckoutConflict) {
		t.Fatal("overlapping checkout", err)
	}
	if _, err := AlertsStripeCheckoutForPerson(app, "00000000-0000-0000-0000-000000000000", first.Intent.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("cross-account lookup", err)
	}
	conflicts, err := personMergeConflicts(ctx, app.DB, "00000000-0000-0000-0000-000000000000", person)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range conflicts {
		if c.Kind == "immutable_account_records" {
			found = true
		}
	}
	if !found {
		t.Fatal("profile merge would reassign immutable checkout")
	}
	createCalls, getCalls := 0, 0
	session := &stripe.CheckoutSession{ID: "cs_test_" + first.Intent.ID, Status: "open", Mode: "subscription", ExpiresAt: time.Now().Add(time.Hour).Unix(), ClientReferenceID: first.Intent.ID, Metadata: map[string]string{"product": "alerts.repository-slots", "account_id": person, "checkout_intent": first.Intent.ID}}
	session.URL = "https://checkout.stripe.com/c/pay/" + session.ID
	client := alertsstripe.Client{Create: func(p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		createCalls++
		if *p.IdempotencyKey != "alerts-checkout:"+first.Intent.ID {
			t.Fatal("missing durable idempotency key")
		}
		return session, nil
	}, Retrieve: func(id string, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		getCalls++
		if id != session.ID {
			t.Fatal(id)
		}
		return session, nil
	}}
	for i := 0; i < 2; i++ {
		destination, err := OpenAlertsStripeCheckout(ctx, app, person, first.Intent.ID, client)
		if err != nil || destination != session.URL {
			t.Fatal(destination, err)
		}
	}
	if createCalls != 1 || getCalls != 1 {
		t.Fatal("provider session not reused", createCalls, getCalls)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET intent=jsonb_set(intent,'{PaidSlots}','3') WHERE id=$1`, first.Intent.ID); err == nil {
		t.Fatal("mutable checkout terms")
	}
	if _, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id='cs_other' WHERE id=$1`, first.Intent.ID); err == nil {
		t.Fatal("reassigned Stripe session")
	}
	start := time.Now().UTC()
	end := start.AddDate(1, 0, 0)
	q := ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Alerts", Provider: "opennode", Quantity: 1, UnitCents: 2400, TotalCents: 2400, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(time.Minute)}
	if _, err := CreateServicePurchase(app, person, "conflicting-prepaid-purchase", q); !errors.Is(err, ErrAlertsStripeCheckoutConflict) {
		t.Fatal("prepaid overlaps Stripe", err)
	}
}
