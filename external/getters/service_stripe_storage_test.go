package getters

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestServiceStripeStorageProductIsolationPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	person := insertSmokePerson(t, app, "shared-stripe-products")
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	alerts, insider := uuid.NewString(), uuid.NewString()
	key := "shared-checkout-request"
	// Same account and request key can independently reserve both products.
	for _, item := range []struct{ id, code string }{{alerts, "alerts.repository-slots"}, {insider, "insider.membership.monthly"}} {
		if _, err := tx.Exec(ctx, `INSERT INTO service_stripe_checkouts(id,person_id,product_code,request_key,intent) VALUES($1,$2,$3,$4,'{}')`, item.id, person, item.code, key); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_checkouts WHERE person_id=$1`, person).Scan(&count); err != nil || count != 1 {
		t.Fatal("Alerts view leaked Insider checkout", count, err)
	}
	// Use savepoints because a rejected SQL statement aborts its transaction.
	rejects := func(query string, args ...any) {
		t.Helper()
		sub, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Rollback(ctx)
		if _, err = sub.Exec(ctx, query, args...); err == nil {
			t.Fatal("accepted invalid cross-product mutation")
		}
	}
	rejects(`INSERT INTO service_stripe_checkouts(id,person_id,product_code,request_key,intent) VALUES($1,$2,'insider.membership.annual','different-request-key','{}')`, uuid.NewString(), person)
	rejects(`UPDATE service_stripe_checkouts SET product_code='alerts.repository-slots' WHERE id=$1`, insider)
	rejects(`INSERT INTO alerts_stripe_checkouts(id,person_id,product_code,request_key,intent) VALUES($1,$2,'insider.membership.annual','invalid-view-request','{}')`, uuid.NewString(), person)
	for _, item := range []struct{ id, subscription string }{{alerts, "sub_" + alerts}, {insider, "sub_" + insider}} {
		if _, err := tx.Exec(ctx, `INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,'{}')`, item.id, item.subscription); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_subscriptions WHERE checkout_id=$1`, insider).Scan(&count); err != nil || count != 0 {
		t.Fatal("Alerts view leaked Insider subscription", count, err)
	}
	result, err := tx.Exec(ctx, `UPDATE alerts_stripe_checkouts SET closed_at=now() WHERE id=$1`, insider)
	if err != nil || result.RowsAffected() != 0 {
		t.Fatal("Alerts adapter closed Insider checkout", err)
	}
	// Use a valid second parent so provider-ID uniqueness, not a missing FK,
	// is what rejects binding one provider subscription to another checkout.
	another := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO service_stripe_checkouts(id,request_key,intent) VALUES($1,'another-valid-request','{}')`, another); err != nil {
		t.Fatal(err)
	}
	rejects(`INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,'{}')`, another, "sub_"+insider)
	for _, id := range []string{alerts, insider} {
		var order string
		if err := tx.QueryRow(ctx, `INSERT INTO shop_orders(public_id,buyer_email,total_cents) VALUES($1,'storage-fixture@example.test',400) RETURNING id::text`, uuid.NewString()).Scan(&order); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO service_stripe_invoices(invoice_id,checkout_id,order_id,paid_period,starts_at,ends_at) VALUES($1,$2,$3,'{}',now(),now()+interval '1 month')`, "in_"+id, id, order); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_invoices WHERE checkout_id=$1`, insider).Scan(&count); err != nil || count != 0 {
		t.Fatal("Alerts view leaked Insider invoice", count, err)
	}
}
