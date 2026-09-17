package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestServiceReceiptQueuePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Receipt queue fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, "DELETE FROM people WHERE id=$1", person); err != nil {
			t.Error(err)
		}
	})
	email := person + "@example.test"
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, email); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	end := now.AddDate(1, 0, 0)
	purchase, err := CreateServicePurchase(app, person, "receipt-queue-test", ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Alerts", Provider: "opennode", Quantity: 1, UnitCents: 2400, TotalCents: 2400, PeriodStart: &now, PeriodEnd: &end, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	a, err := BeginServicePayment(app, person, purchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	ref := "receipt-" + a.ID
	if err = BindServicePayment(app, a.ID, "opennode", ref); err != nil {
		t.Fatal(err)
	}
	// Queue insertion rolls back with settlement evidence.
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE service_payment_attempts SET settled_at=now() WHERE id=$1", a.ID); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = app.DB.QueryRow(ctx, "SELECT count(*) FROM service_receipt_jobs WHERE order_id=$1", purchase.OrderID).Scan(&count); err != nil || count != 0 {
		t.Fatal("rollback left job", count, err)
	}
	payment := VerifiedServicePayment{AttemptID: a.ID, Provider: "opennode", Reference: ref, EventID: "receipt-event-" + a.ID, Currency: "USD", TotalCents: 2400, PaidAt: now}
	for i := 0; i < 2; i++ {
		if err = RecordServicePayment(app, payment); err != nil {
			t.Fatal(err)
		}
	}
	if err = app.DB.QueryRow(ctx, "SELECT count(*) FROM service_receipt_jobs WHERE order_id=$1", purchase.OrderID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate settlement job", count, err)
	}
	at := time.Now().Add(time.Second)
	first, err := ClaimServiceReceipt(ctx, app, purchase.OrderID, at)
	if err != nil || first.Recipient != email || first.PersonID != person {
		t.Fatal(first, err)
	}
	if _, err = ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(time.Minute)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("lease was not exclusive", err)
	}
	second, err := ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(3*time.Minute))
	if err != nil || second.Attempt != 2 {
		t.Fatal(second, err)
	}
	if applied, err := FinishServiceReceipt(ctx, app, first, "accepted", at); err != nil || applied {
		t.Fatal("stale response accepted", applied, err)
	}
	if applied, err := FinishServiceReceipt(ctx, app, second, "retry", at.Add(3*time.Minute)); err != nil || !applied {
		t.Fatal(applied, err)
	}
	if _, err = ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(4*time.Minute)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("retry backoff ignored", err)
	}
	third, err := ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(14*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := FinishServiceReceipt(ctx, app, third, "accepted", at.Add(14*time.Minute)); err != nil || !applied {
		t.Fatal(applied, err)
	}
	if _, err = ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(time.Hour)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("accepted receipt replayed", err)
	}
	if _, err = app.DB.Exec(ctx, "UPDATE service_receipt_jobs SET recipient='other@example.test' WHERE order_id=$1", purchase.OrderID); err == nil {
		t.Fatal("receipt recipient changed")
	}
}
