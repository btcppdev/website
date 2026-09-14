package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestServicePurchasePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Service purchase test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	// Financial snapshots intentionally survive account removal.
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	email := "service-" + databaseSmokeSuffix() + "@example.test"
	start := time.Now().UTC().Truncate(time.Second)
	end := start.AddDate(1, 0, 0)
	quote := ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Alerts repository slots", Provider: "opennode", Quantity: 2, UnitCents: 1200, TotalCents: 2400, TaxCents: 200, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(time.Hour)}
	key := "service-purchase-0001"
	if _, err := CreateServicePurchase(app, person, key, quote); !errors.Is(err, ErrServiceBuyer) {
		t.Fatalf("buyer without verified email: %v", err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,true)`, person, email); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	results := make(chan *ServicePurchase, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := CreateServicePurchase(app, person, key, quote)
			results <- p
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
	var purchase *ServicePurchase
	for result := range results {
		if purchase != nil && *result != *purchase {
			t.Fatal("duplicate order created")
		}
		purchase = result
	}
	changed := quote
	changed.TaxCents++
	if _, err := CreateServicePurchase(app, person, key, changed); !errors.Is(err, ErrServicePurchaseConflict) {
		t.Fatalf("changed quote: %v", err)
	}
	var subtotal, tax, total, items, reservations, snapshots int
	var status string
	if err := app.DB.QueryRow(ctx, `SELECT subtotal_cents,sales_tax_amount_cents,total_cents,status,
		(SELECT count(*) FROM shop_order_items WHERE order_id=o.id),
		(SELECT count(*) FROM merch_inventory_reservations r JOIN shop_order_items i ON i.id=r.order_item_id WHERE i.order_id=o.id),
		(SELECT count(*) FROM service_purchase_requests WHERE person_id=$1)
		FROM shop_orders o WHERE id=$2 AND buyer_person_id=$1`, person, purchase.OrderID).Scan(&subtotal, &tax, &total, &status, &items, &reservations, &snapshots); err != nil {
		t.Fatal(err)
	}
	if subtotal != 2200 || tax != 200 || total != 2400 || status != "pending" || items != 1 || reservations != 0 || snapshots != 1 {
		t.Fatalf("unexpected order: %d %d %d %s items=%d reservations=%d snapshots=%d", subtotal, tax, total, status, items, reservations, snapshots)
	}
	for _, sql := range []string{
		`UPDATE service_purchase_requests SET quote_snapshot='{}' WHERE order_id=$1`,
		`DELETE FROM service_purchase_requests WHERE order_id=$1`,
		`DELETE FROM shop_orders WHERE id=$1`,
	} {
		if _, err := app.DB.Exec(ctx, sql, purchase.OrderID); err == nil {
			t.Fatalf("purchase evidence mutation allowed: %s", sql)
		}
	}
	// Inject a failure at the final insert to exercise transaction rollback.
	if _, err := app.DB.Exec(ctx, `ALTER TABLE service_purchase_requests ADD CONSTRAINT service_purchase_test_reject CHECK(request_key <> 'service-reject-0001') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `ALTER TABLE service_purchase_requests DROP CONSTRAINT service_purchase_test_reject`); err != nil {
			t.Error(err)
		}
	})
	if _, err := CreateServicePurchase(app, person, "service-reject-0001", quote); err == nil {
		t.Fatal("expected injected constraint failure")
	}
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM shop_orders WHERE buyer_person_id=$1`, person).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback: count=%d err=%v", count, err)
	}
	bad := quote
	bad.TotalCents++
	if _, err := CreateServicePurchase(app, person, "service-bad-price-01", bad); !errors.Is(err, ErrServiceQuote) {
		t.Fatalf("invalid total: %v", err)
	}
	bad = quote
	bad.ExpiresAt = start.Add(-time.Hour)
	if _, err := CreateServicePurchase(app, person, "service-expired-0001", bad); !errors.Is(err, ErrServiceQuote) {
		t.Fatalf("expired quote: %v", err)
	}
	var other string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Other service buyer') RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, other); err != nil {
			t.Error(err)
		}
	})
	if _, err := CreateServicePurchase(app, other, key, quote); !errors.Is(err, ErrServiceBuyer) {
		t.Fatalf("cross-account replay: %v", err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,true)`, other, "other-"+email); err != nil {
		t.Fatal(err)
	}
	article := ServiceQuote{ProductCode: "insider.article", ResourceID: "publication-123", Name: "Article access", Provider: "stripe", Quantity: 1, UnitCents: 500, TotalCents: 500, ExpiresAt: quote.ExpiresAt}
	otherPurchase, err := CreateServicePurchase(app, other, key, article)
	if err != nil {
		t.Fatal(err)
	}
	if otherPurchase.OrderID == purchase.OrderID {
		t.Fatal("accounts shared an order")
	}
}
