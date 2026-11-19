package getters

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestOrganizationEventSaleNotifications(t *testing.T) {
	app := postgresSmokeContext(t)
	c := context.Background()
	conf, _ := insertSmokeConference(t, app)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(c, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	org, product, variant := uuid.NewString(), uuid.NewString(), uuid.NewString()
	sales := []string{}
	t.Cleanup(func() {
		for _, sale := range sales {
			app.DB.Exec(c, `DELETE FROM conference_pos_events WHERE sale_id=$1`, sale)
			app.DB.Exec(c, `DELETE FROM conference_pos_sale_items WHERE sale_id=$1`, sale)
			app.DB.Exec(c, `DELETE FROM conference_pos_sales WHERE id=$1`, sale)
		}
		app.DB.Exec(c, `DELETE FROM merch_products WHERE id=$1`, product)
		app.DB.Exec(c, `DELETE FROM organizations WHERE id=$1`, org)
	})
	exec(`INSERT INTO organizations(id,name) VALUES($1,'Event shop')`, org)
	exec(`INSERT INTO merch_products(id,tag,slug,name) VALUES($1::uuid,$1::text,$1::text,'Event tee')`, product)
	exec(`INSERT INTO merch_variants(id,product_id,sku,label) VALUES($1::uuid,$2,$1::text,'Large')`, variant, product)
	newSale := func() string {
		id := uuid.NewString()
		sales = append(sales, id)
		exec(`INSERT INTO conference_pos_sales(id,conference_id,request_id,operator_id,status,total_sats,currency,local_per_btc,charge_id) VALUES($1::uuid,$2,$1::uuid,'test','pending',10000,'USD',100000,$1::text)`, id, conf)
		exec(`INSERT INTO conference_pos_sale_items(sale_id,variant_id,product_name,variant_label,quantity,price_sats,fulfillment_organization_id,organization_fee_bps,organization_fee_sats,supplier_amount_sats) VALUES($1,$2,'Event tee','Large',2,5000,$3,1500,1500,8500)`, id, variant, org)
		return id
	}
	count := func(id string) int {
		t.Helper()
		var n int
		if err := app.DB.QueryRow(c, `SELECT count(*) FROM organization_sale_notifications WHERE pos_sale_id=$1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	sale := newSale()
	if count(sale) != 0 {
		t.Fatal("pending event sale notified")
	}
	if err := POSApplyPayment(app, sale, sale, "paid", 9999); err == nil {
		t.Fatal("wrong payment accepted")
	}
	if count(sale) != 0 {
		t.Fatal("invalid payment notified")
	}
	for range 2 {
		if err := POSApplyPayment(app, sale, sale, "paid", 10000); err != nil {
			t.Fatal(err)
		}
	}
	if count(sale) != 1 {
		t.Fatal("event replay duplicated notice")
	}
	var id string
	if err := app.DB.QueryRow(c, `SELECT id::text FROM organization_sale_notifications WHERE pos_sale_id=$1`, sale).Scan(&id); err != nil {
		t.Fatal(err)
	}
	n, err := GetOrganizationSaleNotice(app, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.OrganizationID != org || n.Currency != "sats" || n.SupplierAmount != 8500 || !n.HasSupplierAmount || n.Event != "Smoke Test Conf" || len(n.Items) != 1 || n.Items[0].Quantity != 2 || n.Items[0].Fulfillment != "pos_takeaway" {
		t.Fatalf("event notice %+v", n)
	}
	expired := newSale()
	if err := POSApplyPayment(app, expired, expired, "expired", 10000); err != nil {
		t.Fatal(err)
	}
	if err := POSApplyPayment(app, expired, expired, "paid", 10000); err != nil {
		t.Fatal(err)
	}
	if count(expired) != 0 {
		t.Fatal("expired/late payment awaiting review notified as a sale")
	}
}
