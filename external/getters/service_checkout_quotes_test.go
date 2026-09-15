package getters

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestStoredServiceCheckoutQuotePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	createPerson := func() string {
		t.Helper()
		var person string
		if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Local stored checkout') RETURNING id::text`).Scan(&person); err != nil {
			t.Fatal(err)
		}
		if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "stored-"+person+"@example.test"); err != nil {
			t.Fatal(err)
		}
		return person
	}
	person := createPerson()
	other := createPerson()
	revision, err := AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Second)
	end := start.AddDate(1, 0, 0)
	expiry := start.Add(10 * time.Minute)
	quote := ServiceQuote{AccountRevision: revision, BillingMonths: 12, ProductCode: "alerts.repository-slots", Name: "Alerts additional projects", Provider: "opennode", Quantity: 2, UnitCents: 1200, TotalCents: 2400, TaxCents: 200, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: expiry}
	tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_stored_" + person, SalesTaxAmountCents: 200, DestinationCountry: "US", RawResponse: `{"amount_total":2400}`, ExpiresAt: &expiry}
	stored, err := SaveAlertsCheckoutQuote(app, person, quote, tax)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := SaveAlertsCheckoutQuote(app, person, quote, tax)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmServiceCheckoutQuote(app, other, stored.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("another account could confirm quote", err)
	}
	if _, err := ServiceCheckoutQuoteForPerson(app, person, "invalid-id"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("invalid quote ID", err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_checkout_quotes SET quote_snapshot=jsonb_set(quote_snapshot,'{total_cents}','1') WHERE id=$1`, stored.ID); err == nil {
		t.Fatal("stored price could be replaced")
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_checkout_quotes SET person_id=$2 WHERE id=$1`, stored.ID, other); err == nil {
		t.Fatal("quote could be transferred")
	}
	var wg sync.WaitGroup
	ids := make(chan string, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			purchase, err := ConfirmServiceCheckoutQuote(app, person, stored.ID)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- purchase.OrderID
		}()
	}
	wg.Wait()
	close(ids)
	var orderID string
	for id := range ids {
		if orderID != "" && orderID != id {
			t.Fatal("duplicate confirmation orders")
		}
		orderID = id
	}
	if orderID == "" {
		t.Fatal("no confirmed order")
	}
	if _, err := ConfirmServiceCheckoutQuote(app, person, stale.ID); !errors.Is(err, ErrAlertsBillingChanged) {
		t.Fatal("stale stored quote accepted", err)
	}
	var orders, attempts, taxes int
	if err := app.DB.QueryRow(ctx, `SELECT count(*),(SELECT count(*) FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.person_id=$1),(SELECT count(*) FROM tax_quotes t JOIN shop_orders o ON o.id=t.order_id WHERE o.buyer_person_id=$1) FROM service_purchase_requests WHERE person_id=$1`, person).Scan(&orders, &attempts, &taxes); err != nil || orders != 1 || attempts != 0 || taxes != 1 {
		t.Fatal("confirmation side effects", orders, attempts, taxes, err)
	}
	loaded, err := ServiceCheckoutQuoteForPerson(app, person, stored.ID)
	if err != nil || loaded.Quote.TotalCents != 2400 || loaded.Tax.ProviderQuoteID != tax.ProviderQuoteID {
		t.Fatal("reviewed terms not preserved", loaded, err)
	}
	// Simulate an unconfirmed quote that has aged past its original expiry.
	quote.ExpiresAt = start.Add(-time.Minute)
	if _, err := SaveAlertsCheckoutQuote(app, person, quote, tax); !errors.Is(err, ErrServiceQuote) {
		t.Fatal("stored an already expired quote", err)
	}
	quote.AccountRevision, err = AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	rawQuote, _ := json.Marshal(quote)
	rawTax, _ := json.Marshal(tax)
	var expiredID string
	if err := app.DB.QueryRow(ctx, `INSERT INTO service_checkout_quotes(person_id,quote_snapshot,tax_snapshot,expires_at) VALUES($1,$2,$3,$4) RETURNING id::text`, person, rawQuote, rawTax, quote.ExpiresAt).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfirmServiceCheckoutQuote(app, person, expiredID); !errors.Is(err, ErrServiceQuote) {
		t.Fatal("expired quote confirmed", err)
	}
}
