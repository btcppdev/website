package getters

import (
	"context"
	"errors"
	"github.com/stripe/stripe-go/v86"
	"os"
	"sync"
	"testing"
	"time"
)

func TestTaxedServicePurchasePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Local inclusive tax purchase') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "tax-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Second)
	end := start.AddDate(1, 0, 0)
	expires := start.Add(10 * time.Minute)
	quote := ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Alerts additional projects", Provider: "opennode", Quantity: 2, UnitCents: 1200, TotalCents: 2400, TaxCents: 200, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: expires}
	tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_local_" + person, SalesTaxAmountCents: 200, DestinationCountry: "US", DestinationRegion: "TX", RawResponse: `{"amount_total":2400,"tax_amount_inclusive":200}`, ExpiresAt: &expires}
	purchase, err := CreateTaxedServicePurchase(app, person, "taxed-service-purchase", quote, tax)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := CreateTaxedServicePurchase(app, person, "taxed-service-purchase", quote, tax)
	if err != nil || retry.OrderID != purchase.OrderID {
		t.Fatal("replay", err)
	}
	changed := tax
	changed.ProviderQuoteID += "-changed"
	if _, err := CreateTaxedServicePurchase(app, person, "taxed-service-purchase", quote, changed); !errors.Is(err, ErrServicePurchaseConflict) {
		t.Fatal("tax evidence replaced", err)
	}
	var total, subtotal, amount int
	if err := app.DB.QueryRow(ctx, `SELECT o.total_cents,o.subtotal_cents,t.sales_tax_amount_cents FROM shop_orders o JOIN tax_quotes t ON t.order_id=o.id WHERE o.id=$1`, purchase.OrderID).Scan(&total, &subtotal, &amount); err != nil || total != 2400 || subtotal != 2200 || amount != 200 {
		t.Fatal("inclusive totals", total, subtotal, amount, err)
	}
	var minimized bool
	if err = app.DB.QueryRow(ctx, `SELECT destination_country='' AND destination_region='' AND destination_postal_code='' AND raw_tax_response='{}'::jsonb FROM tax_quotes WHERE order_id=$1`, purchase.OrderID).Scan(&minimized); err != nil || !minimized {
		t.Fatal("order tax retained address", err)
	}
	// A provider reference that PostgreSQL cannot represent forces failure at tax storage,
	// after the order has been inserted inside the transaction.
	broken := tax
	broken.ProviderQuoteID = "taxcalc_bad\x00"
	if _, err := CreateTaxedServicePurchase(app, person, "taxed-service-rollback", quote, broken); err == nil {
		t.Fatal("expected tax storage failure")
	}
	var orders, taxes int
	if err := app.DB.QueryRow(ctx, `SELECT count(*),count(t.id) FROM shop_orders o LEFT JOIN tax_quotes t ON t.order_id=o.id WHERE o.buyer_person_id=$1`, person).Scan(&orders, &taxes); err != nil || orders != 1 || taxes != 1 {
		t.Fatal("partial purchase or duplicate tax record", orders, taxes, err)
	}
	called := 0
	create := func(p *stripe.TaxTransactionCreateFromCalculationParams) (*stripe.TaxTransaction, error) {
		called++
		if *p.Calculation != tax.ProviderQuoteID || *p.IdempotencyKey != "service-tax-transaction:"+purchase.OrderID || *p.PostedAt != start.Unix() {
			t.Error("incorrect tax transaction request")
		}
		return &stripe.TaxTransaction{CustomerDetails: &stripe.TaxTransactionCustomerDetails{Address: &stripe.Address{Line1: "PRIVATE STREET"}}, ID: "tax_local_" + person, Reference: "service-order-" + purchase.OrderID, Currency: "usd", Type: "transaction", LineItems: &stripe.TaxTransactionLineItemList{Data: []*stripe.TaxTransactionLineItem{{Amount: 2400, AmountTax: 200, Quantity: 2, Reference: "alerts.repository-slots", TaxBehavior: "inclusive"}}}}, nil
	}
	if err := finalizeServiceTax(ctx, app, purchase.OrderID, create); err == nil || called != 0 {
		t.Fatal("unpaid tax transaction recorded")
	}
	attempt, err := BeginServicePayment(app, person, purchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if err := BindServicePayment(app, attempt.ID, "opennode", "local-tax-payment-"+person); err != nil {
		t.Fatal(err)
	}
	if err := RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: "opennode", Reference: "local-tax-payment-" + person, EventID: "local-tax-event-" + person, Currency: "USD", TotalCents: 2400, PaidAt: start}); err != nil {
		t.Fatal(err)
	}
	if _, err := FulfillServicePurchase(app, purchase.OrderID); err != nil {
		t.Fatal(err)
	}
	if err := finalizeServiceTax(ctx, app, purchase.OrderID, func(*stripe.TaxTransactionCreateFromCalculationParams) (*stripe.TaxTransaction, error) {
		return nil, errors.New("temporary tax outage")
	}); err == nil {
		t.Fatal("tax outage ignored")
	}
	active, err := ActiveServiceEntitlements(app, person, start.Add(time.Second))
	if err != nil || len(active) != 1 {
		t.Fatal("tax outage blocked access", err)
	}
	if err := finalizeServiceTax(ctx, app, purchase.OrderID, func(p *stripe.TaxTransactionCreateFromCalculationParams) (*stripe.TaxTransaction, error) {
		result, err := create(p)
		result.LineItems.Data[0].Amount++
		return result, err
	}); err == nil {
		t.Fatal("mismatched tax transaction recorded")
	}
	called = 0
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := finalizeServiceTax(ctx, app, purchase.OrderID, create); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if called != 1 {
		t.Fatal("duplicate tax transaction requests", called)
	}
	var records int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM tax_transactions WHERE order_id=$1`, purchase.OrderID).Scan(&records); err != nil || records != 1 {
		t.Fatal("tax transaction missing or duplicated", records, err)
	}
	if err = app.DB.QueryRow(ctx, `SELECT raw_response='{}'::jsonb FROM tax_transactions WHERE order_id=$1`, purchase.OrderID).Scan(&minimized); err != nil || !minimized {
		t.Fatal("transaction retained address", err)
	}
	// Simulate historical data, apply the cleanup and check both minimization
	// and preservation of the financial identifiers. Roll back the fixture.
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, sql := range []string{
		`ALTER TABLE tax_quotes DISABLE TRIGGER alerts_tax_address_minimization`,
		`ALTER TABLE tax_transactions DISABLE TRIGGER alerts_tax_address_minimization`,
		`UPDATE tax_quotes SET destination_country='US',destination_region='PRIVATE REGION',destination_postal_code='PRIVATE POSTAL',raw_tax_response='{"address":"PRIVATE STREET"}' WHERE order_id='` + purchase.OrderID + `'`,
		`UPDATE tax_transactions SET raw_response='{"address":"PRIVATE STREET"}' WHERE order_id='` + purchase.OrderID + `'`,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../db/migrations/149_alerts_address_minimization.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT q.destination_country='' AND q.destination_region='' AND q.destination_postal_code='' AND q.raw_tax_response='{}'::jsonb AND t.raw_response='{}'::jsonb AND q.provider_quote_id=$2 AND q.sales_tax_amount_cents=200 AND t.sales_tax_amount_cents=200 FROM tax_quotes q JOIN tax_transactions t USING(order_id) WHERE q.order_id=$1`, purchase.OrderID, tax.ProviderQuoteID).Scan(&minimized); err != nil || !minimized {
		t.Fatal("historical cleanup changed financial evidence or retained address", err)
	}

}
