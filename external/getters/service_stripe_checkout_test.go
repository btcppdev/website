package getters

import (
	"btcpp-web/internal/servicestripe"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
	"sync"
	"testing"
	"time"
)

func TestServiceStripeCheckoutPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Shared checkout test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	terms := servicestripe.CheckoutIntent{ProductCode: "insider.membership.monthly", Quantity: 1, TaxCode: "txcd_test", ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/account/success", CancelURL: "https://insider.btcpp.dev/account/cancel"}
	if _, err := ReserveServiceStripeCheckout(app, person, "shared-checkout-test-001", terms); !errors.Is(err, ErrServiceBuyer) {
		t.Fatal("unverified buyer", err)
	}
	email := "shared-" + person + "@example.test"
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, email); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan *ServiceStripeCheckout, 5)
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := ReserveServiceStripeCheckout(app, person, "shared-checkout-test-001", terms)
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
	var first *ServiceStripeCheckout
	for out := range results {
		if first != nil && first.Intent.ID != out.Intent.ID {
			t.Fatal("duplicate reservation")
		}
		first = out
	}
	if first.Intent.Email != email {
		t.Fatal("email not loaded from account")
	}
	changed := terms
	changed.ProductCode = "insider.membership.annual"
	if _, err := ReserveServiceStripeCheckout(app, person, "another-checkout-test-001", changed); !errors.Is(err, ErrServiceStripeCheckoutConflict) {
		t.Fatal("overlapping membership", err)
	}
	for _, scope := range [][2]string{{"00000000-0000-0000-0000-000000000000", terms.ProductCode}, {person, "insider.membership.annual"}, {person, "alerts.repository-slots"}} {
		if _, err := ServiceStripeCheckoutForPerson(app, scope[0], first.Intent.ID, scope[1]); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("scope leak", err)
		}
	}
	if _, err := ReserveAlertsStripeCheckout(app, person, "shared-checkout-test-001", 1, "https://btcpp.dev"); err != nil {
		t.Fatal("independent Alerts checkout", err)
	}
	start := time.Now().UTC()
	end := start.AddDate(1, 0, 0)
	quote := ServiceQuote{ProductCode: "insider.membership.annual", Name: "Insider", Provider: "opennode", Quantity: 1, UnitCents: 21000, TotalCents: 21000, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(time.Hour)}
	if _, err := CreateServicePurchase(app, person, "conflicting-prepaid-test", quote); !errors.Is(err, ErrServiceStripeCheckoutConflict) {
		t.Fatal("prepaid overlaps recurring", err)
	}
	created, retrieved := 0, 0
	session := &stripe.CheckoutSession{ID: "cs_test_" + first.Intent.ID, Status: "open", Mode: "subscription", ExpiresAt: time.Now().Add(time.Hour).Unix(), ClientReferenceID: first.Intent.ID, Metadata: map[string]string{"account_id": person, "product": terms.ProductCode, "checkout_intent": first.Intent.ID}}
	session.URL = "https://checkout.stripe.com/c/pay/" + session.ID
	client := servicestripe.CheckoutClient{Create: func(p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		created++
		if *p.IdempotencyKey != "service-checkout:"+first.Intent.ID {
			t.Fatal("idempotency")
		}
		return session, nil
	}, Retrieve: func(id string, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		retrieved++
		if id != session.ID {
			t.Fatal(id)
		}
		return session, nil
	}}
	for i := 0; i < 2; i++ {
		if url, err := OpenServiceStripeCheckout(ctx, app, person, first.Intent.ID, terms.ProductCode, client); err != nil || url != session.URL {
			t.Fatal(url, err)
		}
	}
	if created != 1 || retrieved != 1 {
		t.Fatal("session not reused", created, retrieved)
	}
	session.Status = "complete"
	if _, err := OpenServiceStripeCheckout(ctx, app, person, first.Intent.ID, terms.ProductCode, client); !errors.Is(err, servicestripe.ErrReconcile) {
		t.Fatal("completed session replaced", err)
	}

	session.Customer = &stripe.Customer{ID: "cus_" + person}
	session.Subscription = &stripe.Subscription{ID: "sub_" + first.Intent.ID}
	price := &stripe.Price{ID: "price_" + first.Intent.ID, Currency: "usd", UnitAmount: 2100, TaxBehavior: "exclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "month", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_" + first.Intent.ID, TaxCode: &stripe.TaxCode{ID: terms.TaxCode}}}
	session.LineItems = &stripe.LineItemList{Data: []*stripe.LineItem{{Quantity: 1, Price: price}}}
	for i := 0; i < 2; i++ {
		bound, err := ResolveServiceStripeSubscription(ctx, app, person, first.Intent.ID, terms.ProductCode, client)
		if err != nil || bound.SubscriptionID != session.Subscription.ID {
			t.Fatal("subscription binding", bound, err)
		}
	}
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_stripe_subscriptions WHERE checkout_id=$1`, first.Intent.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("binding not deduplicated", count, err)
	}
	grants, err := ActiveServiceEntitlements(app, person, time.Now())
	if err != nil || len(grants) != 0 {
		t.Fatal("binding granted access", grants, err)
	}
	session.Subscription.ID = "sub_replaced"
	if _, err := ResolveServiceStripeSubscription(ctx, app, person, first.Intent.ID, terms.ProductCode, client); err == nil {
		t.Fatal("binding overwritten")
	}

}
