package getters

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestPurchaseInvoiceOwnershipSnapshotAndRetries(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	owner, other, ref := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO people(id,name) VALUES($1,'Invoice owner'),($2,'Other account')`, owner, other)
	var order string
	if err := app.DB.QueryRow(ctx, `INSERT INTO shop_orders(public_id,buyer_email,buyer_person_id,status,subtotal_cents,sales_tax_amount_cents,total_cents,payment_provider) VALUES($1,'invoice@example.test',$2,'paid',1000,100,1100,'stripe') RETURNING id::text`, ref, owner).Scan(&order); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO shop_order_items(order_id,quantity,unit_price_cents,line_total_cents,product_name_snapshot) VALUES($1,1,1000,1000,'Event ticket')`, order)
	t.Cleanup(func() {
		exec(`DELETE FROM purchase_invoices WHERE order_id=$1`, order)
		exec(`DELETE FROM shop_orders WHERE id=$1`, order)
		exec(`DELETE FROM people WHERE id IN ($1,$2)`, owner, other)
	})
	buyer := InvoiceBuyer{Name: "Example Co", Address: "123 Test St", City: "Austin", PostalCode: "78702", Country: "USA", TaxID: "TEST-123"}
	if _, err := IssuePurchaseInvoice(ctx, app, other, ref, buyer); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign issue: %v", err)
	}
	if _, err := IssuePurchaseInvoice(ctx, app, owner, ref, InvoiceBuyer{}); !errors.Is(err, ErrInvoiceDetails) {
		t.Fatalf("missing details: %v", err)
	}
	var wg sync.WaitGroup
	ids := make(chan int64, 4)
	errs := make(chan error, 4)
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			i, err := IssuePurchaseInvoice(ctx, app, owner, ref, buyer)
			if err != nil {
				errs <- err
				return
			}
			ids <- i.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var id int64
	for got := range ids {
		if id != 0 && got != id {
			t.Fatal("duplicate invoice")
		}
		id = got
	}
	invoice, err := PurchaseInvoiceForPerson(ctx, app, owner, ref)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Snapshot.Tax != 100 || invoice.Snapshot.Total != 1100 || invoice.Snapshot.SellerEIN != "93-4644668" || len(invoice.Snapshot.Items) != 1 {
		t.Fatal("incorrect invoice snapshot")
	}
	// Service line prices include tax; invoice lines must not count it twice.
	svcRef := uuid.NewString()
	var svcOrder string
	if err := app.DB.QueryRow(ctx, `INSERT INTO shop_orders(public_id,buyer_email,buyer_person_id,status,checkout_kind,subtotal_cents,sales_tax_amount_cents,total_cents) VALUES($1,'invoice@example.test',$2,'paid','service',1000,100,1100) RETURNING id::text`, svcRef, owner).Scan(&svcOrder); err != nil {
		t.Fatal(err)
	}
	defer func() {
		exec(`DELETE FROM purchase_invoices WHERE order_id=$1`, svcOrder)
		exec(`DELETE FROM shop_orders WHERE id=$1`, svcOrder)
	}()
	exec(`INSERT INTO shop_order_items(order_id,quantity,unit_price_cents,tax_amount_cents,line_total_cents,product_name_snapshot,fulfillment_method,service_product_code,service_period_start,service_period_end) VALUES($1,1,1100,100,1100,'Software subscription','service','alerts',now(),now()+interval '1 month')`, svcOrder)
	svc, err := IssuePurchaseInvoice(ctx, app, owner, svcRef, buyer)
	if err != nil || svc.Snapshot.Items[0].Amount != 1000 {
		t.Fatal("inclusive tax double counted", err)
	}
	original, _ := json.Marshal(invoice.Snapshot)
	exec(`UPDATE shop_orders SET status='refunded',buyer_name='Changed' WHERE id=$1`, order)
	changed := buyer
	changed.Name = "Different company"
	again, err := IssuePurchaseInvoice(ctx, app, owner, ref, changed)
	if err != nil {
		t.Fatal(err)
	}
	frozen, _ := json.Marshal(again.Snapshot)
	if string(frozen) != string(original) || again.ID != id {
		t.Fatal("issued invoice rewritten")
	}
	if _, err := PurchaseInvoiceForPerson(ctx, app, other, ref); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign read", err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE purchase_invoices SET snapshot='{}' WHERE id=$1`, id); err == nil {
		t.Fatal("snapshot mutation accepted")
	}
	exec(`UPDATE people SET is_deleted_account=true WHERE id=$1`, owner)
	if _, err := PurchaseInvoiceForPerson(ctx, app, owner, ref); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("deleted account read", err)
	}
}

func TestPurchaseInvoiceRejectsPendingOrInconsistentOrders(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	person, ref := uuid.NewString(), uuid.NewString()
	_, err := app.DB.Exec(ctx, `INSERT INTO people(id,name) VALUES($1,'Invoice fixture')`, person)
	if err != nil {
		t.Fatal(err)
	}
	var order string
	err = app.DB.QueryRow(ctx, `INSERT INTO shop_orders(public_id,buyer_email,buyer_person_id,total_cents) VALUES($1,'invoice@example.test',$2,100) RETURNING id::text`, ref, person).Scan(&order)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.DB.Exec(ctx, `DELETE FROM shop_orders WHERE id=$1`, order)
		app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
	})
	buyer := InvoiceBuyer{Name: "Buyer", Address: "Street", City: "Austin", PostalCode: "78702", Country: "USA"}
	if _, err = IssuePurchaseInvoice(ctx, app, person, ref, buyer); !errors.Is(err, ErrInvoiceUnavailable) {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE shop_orders SET status='paid' WHERE id=$1`, order); err != nil {
		t.Fatal(err)
	}
	if _, err = IssuePurchaseInvoice(ctx, app, person, ref, buyer); !errors.Is(err, ErrInvoiceUnavailable) {
		t.Fatal("inconsistent order accepted", err)
	}
}
