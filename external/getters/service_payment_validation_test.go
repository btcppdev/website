package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestServiceChargeRejectsEditedPurchasePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Local charge validation test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "validation-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []string{
		`UPDATE shop_orders SET total_cents=total_cents+1 WHERE id=$1`,
		`UPDATE shop_orders SET currency='EUR' WHERE id=$1`,
		`UPDATE shop_orders SET shipping_amount_cents=1 WHERE id=$1`,
		`UPDATE shop_orders SET payment_provider='stripe' WHERE id=$1`,
		`UPDATE shop_order_items SET quantity=2 WHERE order_id=$1`,
		`UPDATE shop_order_items SET product_name_snapshot='Changed product' WHERE order_id=$1`,
		`UPDATE shop_order_items SET service_resource_id='another-article' WHERE order_id=$1`,
		`UPDATE shop_order_items SET tax_amount_cents=1 WHERE order_id=$1`,
		`DELETE FROM shop_order_items WHERE order_id=$1`,
	} {
		quote := ServiceQuote{ProductCode: "insider.article", ResourceID: "validation", Name: "Original product", Provider: "opennode", Quantity: 1, UnitCents: 500, TotalCents: 500, ExpiresAt: time.Now().Add(time.Hour)}
		purchase, err := CreateServicePurchase(app, person, uuid.NewString(), quote)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.DB.Exec(ctx, edit, purchase.OrderID); err != nil {
			t.Fatal(err)
		}
		if _, err := BeginServicePayment(app, person, purchase.OrderID); !errors.Is(err, ErrServicePayment) {
			t.Fatalf("edited purchase accepted (%s): %v", edit, err)
		}
		var attempts int
		if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.order_id=$1`, purchase.OrderID).Scan(&attempts); err != nil || attempts != 0 {
			t.Fatal("rejected purchase allocated an attempt", attempts, err)
		}
	}
}
