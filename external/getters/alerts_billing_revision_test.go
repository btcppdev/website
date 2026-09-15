package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAlertsBillingRevisionPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Local billing revision') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "revision-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	revision, err := AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Second)
	end := start.AddDate(1, 0, 0)
	quote := ServiceQuote{AccountRevision: revision, ProductCode: "alerts.repository-slots", Name: "Alerts", Provider: "opennode", Quantity: 1, UnitCents: 1200, TotalCents: 1200, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(10 * time.Minute)}
	// Two independently confirmed quotes from the same revision cannot both create
	// purchases. This also exercises the account lock across concurrent callers.
	var wg sync.WaitGroup
	results := make(chan *ServicePurchase, 2)
	failures := make(chan error, 2)
	for _, key := range []string{"revision-purchase-one", "revision-purchase-two"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			p, e := CreateServicePurchase(app, person, key, quote)
			if e != nil {
				failures <- e
			} else {
				results <- p
			}
		}(key)
	}
	wg.Wait()
	close(results)
	close(failures)
	if len(results) != 1 || len(failures) != 1 {
		t.Fatalf("created %d purchases, %d failures", len(results), len(failures))
	}
	if err := <-failures; !errors.Is(err, ErrAlertsBillingChanged) {
		t.Fatal(err)
	}
	purchase := <-results
	var key string
	if err := app.DB.QueryRow(ctx, `SELECT request_key FROM service_purchase_requests WHERE order_id=$1`, purchase.OrderID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	retry, err := CreateServicePurchase(app, person, key, quote)
	if err != nil || retry.OrderID != purchase.OrderID {
		t.Fatal("idempotent retry failed after revision changed", err)
	}
	pending, err := AlertsBillingRevision(app, person)
	if err != nil || pending == revision {
		t.Fatal("pending order did not change revision", err)
	}
	quote.AccountRevision = pending
	if _, err := app.DB.Exec(ctx, `UPDATE shop_orders SET status='cancelled' WHERE id=$1`, purchase.OrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateServicePurchase(app, person, "revision-after-cancel", quote); !errors.Is(err, ErrAlertsBillingChanged) {
		t.Fatal("cancelled order did not invalidate quote", err)
	}
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_purchase_requests WHERE person_id=$1`, person).Scan(&count); err != nil || count != 1 {
		t.Fatal("stale quote created purchase", count, err)
	}

	quote.AccountRevision, err = AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := CreateServicePurchase(app, person, "revision-fresh-purchase", quote)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := BeginServicePayment(app, person, fresh.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	reference := "revision-payment-" + person
	if err := BindServicePayment(app, attempt.ID, "opennode", reference); err != nil {
		t.Fatal(err)
	}
	if err := RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: "opennode", Reference: reference, EventID: "revision-event-" + person, Currency: "USD", TotalCents: 1200, PaidAt: start}); err != nil {
		t.Fatal(err)
	}
	paid, err := AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := FulfillServicePurchase(app, fresh.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	active, err := AlertsBillingRevision(app, person)
	if err != nil || active == paid {
		t.Fatal("fulfillment did not change revision", err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_entitlements SET revoked_at=now(),revocation_actor='local test',revocation_reason='revision verification' WHERE id=$1`, grant); err != nil {
		t.Fatal(err)
	}
	revoked, err := AlertsBillingRevision(app, person)
	if err != nil || revoked == active {
		t.Fatal("revocation did not change revision", err)
	}
	state, err := LoadAlertsBillingState(app, person)
	if err != nil || state.Revision != revoked || len(state.Orders) != 2 {
		t.Fatal("state and revision diverged", state, err)
	}
	var found bool
	for _, order := range state.Orders {
		if order.OrderID == fresh.OrderID {
			found = order.Granted && order.Revoked && order.PaymentStarted && order.Quote.Quantity == 1
		}
	}
	if !found {
		t.Fatal("billing state lost payment/grant evidence")
	}
	// Empty account histories must still produce account-specific revisions.
	var other string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Other revision account') RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	otherRevision, err := AlertsBillingRevision(app, other)
	if err != nil || otherRevision == revision {
		t.Fatal("revision not account scoped", err)
	}
}
