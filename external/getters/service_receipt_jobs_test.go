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
	assertReceiptMetrics := func() {
		t.Helper()
		counts, err := ListBusinessMetricCounts(app)
		if err != nil {
			t.Fatal(err)
		}
		states := 0
		oldest := false
		for _, metric := range counts {
			if metric.Metric == "service_receipt_jobs" {
				var want float64
				if err := app.DB.QueryRow(ctx, "SELECT count(*)::float8 FROM service_receipt_jobs WHERE state=$1", metric.State).Scan(&want); err != nil || metric.Count != want {
					t.Fatalf("receipt state %s: got %v want %v: %v", metric.State, metric.Count, want, err)
				}
				states++
			}
			if metric.Metric == "service_receipt_oldest_pending_timestamp_seconds" {
				var want float64
				if err := app.DB.QueryRow(ctx, "SELECT coalesce(extract(epoch FROM min(created_at)),0)::float8 FROM service_receipt_jobs WHERE state='pending'").Scan(&want); err != nil || metric.Count != want {
					t.Fatalf("oldest pending: got %v want %v: %v", metric.Count, want, err)
				}
				oldest = true
			}
		}
		if states != 4 || !oldest {
			t.Fatalf("missing receipt metrics: states=%d oldest=%v", states, oldest)
		}
	}
	assertReceiptMetrics()
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
	if applied, err := FinishServiceReceipt(ctx, app, third, "review", time.Now()); err != nil || !applied {
		t.Fatal(applied, err)
	}
	if applied, err := RecoverServiceReceipt(ctx, app, purchase.PublicID, third.Attempt-1, "fixture", "mailer repaired"); err != nil || applied {
		t.Fatal("stale recovery applied", applied, err)
	}
	if applied, err := RecoverServiceReceipt(ctx, app, purchase.PublicID, third.Attempt, "fixture", "mailer repaired"); err != nil || !applied {
		t.Fatal("recovery failed", applied, err)
	}
	if applied, err := RecoverServiceReceipt(ctx, app, purchase.PublicID, third.Attempt, "fixture", "duplicate"); err != nil || applied {
		t.Fatal("duplicate recovery applied", applied, err)
	}
	if applied, err := FinishServiceReceipt(ctx, app, third, "accepted", time.Now()); err != nil || applied {
		t.Fatal("pre-recovery response accepted", applied, err)
	}
	statuses, err := ListServiceReceipts(ctx, app, purchase.PublicID)
	if err != nil || len(statuses) != 1 || statuses[0].State != "pending" || statuses[0].RetryAfterAttempt != third.Attempt {
		t.Fatal("recovery inspection", statuses, err)
	}
	if err := app.DB.QueryRow(ctx, "SELECT count(*) FROM service_receipt_recoveries WHERE order_id=$1 AND actor='fixture' AND reason='mailer repaired'", purchase.OrderID).Scan(&count); err != nil || count != 1 {
		t.Fatal("recovery audit missing", count, err)
	}
	recovered, err := ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(15*time.Minute))
	if err != nil || recovered.Attempt != third.Attempt+1 || recovered.Recipient != first.Recipient || recovered.PublicID != first.PublicID {
		t.Fatal("recovery changed identity or did not advance attempt", recovered, err)
	}
	// A recovery grants exactly twelve additional attempts, without resetting the
	// monotonic attempt number used to fence earlier workers.
	for i := 0; i < 12; i++ {
		if i > 0 {
			recovered, err = ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(time.Duration(i)*7*time.Hour))
			if err != nil {
				t.Fatal("recovery budget exhausted early", i, err)
			}
		}
		if applied, err := FinishServiceReceipt(ctx, app, recovered, "retry", time.Now()); err != nil || !applied {
			t.Fatal("retry finish", applied, err)
		}
	}
	if _, err := ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(100*time.Hour)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("recovery exceeded bounded budget", err)
	}
	statuses, err = ListServiceReceipts(ctx, app, purchase.PublicID)
	if err != nil || len(statuses) != 1 || statuses[0].State != "review" || statuses[0].Attempts != third.Attempt+12 {
		t.Fatal("exhausted receipt not in review", statuses, err)
	}
	if applied, err := RecoverServiceReceipt(ctx, app, purchase.PublicID, recovered.Attempt, "fixture", "second repair"); err != nil || !applied {
		t.Fatal("second bounded recovery", applied, err)
	}
	recovered, err = ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(101*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := FinishServiceReceipt(ctx, app, recovered, "accepted", at.Add(101*time.Hour)); err != nil || !applied {
		t.Fatal(applied, err)
	}
	if applied, err := RecoverServiceReceipt(ctx, app, purchase.PublicID, recovered.Attempt, "fixture", "already accepted"); err != nil || applied {
		t.Fatal("accepted receipt recovered", applied, err)
	}
	if _, err = ClaimServiceReceipt(ctx, app, purchase.OrderID, at.Add(time.Hour)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("accepted receipt replayed", err)
	}
	assertReceiptMetrics()
	if _, err = app.DB.Exec(ctx, "UPDATE service_receipt_jobs SET recipient='other@example.test' WHERE order_id=$1", purchase.OrderID); err == nil {
		t.Fatal("receipt recipient changed")
	}
}

func TestServiceReceiptDeletedAccountPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	for _, state := range []string{"pending", "review", "accepted"} {
		t.Run(state, func(t *testing.T) {
			person := insertSmokePerson(t, app, "receipt-deleted-"+state)
			now := time.Now().UTC().Truncate(time.Microsecond)
			end := now.AddDate(1, 0, 0)
			purchase, err := CreateServicePurchase(app, person, "deleted-receipt-fixture", ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Alerts", Provider: "opennode", Quantity: 1, UnitCents: 2400, TotalCents: 2400, PeriodStart: &now, PeriodEnd: &end, ExpiresAt: now.Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			// Make recovery otherwise eligible; account removal must be the reason
			// a reviewed job cannot be requeued.
			if _, err := app.DB.Exec(ctx, `UPDATE shop_orders SET status='paid' WHERE id=$1`, purchase.OrderID); err != nil {
				t.Fatal(err)
			}
			var accepted *time.Time
			if state == "accepted" {
				accepted = &now
			}
			_, err = app.DB.Exec(ctx, `INSERT INTO service_receipt_jobs(order_id,recipient,state,attempts,accepted_at) VALUES($1,'deleted@example.test',$2,1,$3)`, purchase.OrderID, state, accepted)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
				t.Fatal(err)
			}
			if applied, err := RecoverServiceReceipt(ctx, app, purchase.PublicID, 1, "fixture", "account removed"); err != nil || applied {
				t.Fatal("deleted account recovered", applied, err)
			}
			for i := 0; i < 2; i++ {
				if _, err := ClaimServiceReceipt(ctx, app, purchase.OrderID, now.Add(time.Hour)); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatal("deleted account claimed", err)
				}
			}
			statuses, err := ListServiceReceipts(ctx, app, purchase.PublicID)
			want := "cancelled"
			if state == "accepted" {
				want = "accepted"
			}
			if err != nil || len(statuses) != 1 || statuses[0].State != want || statuses[0].Attempts != 1 {
				t.Fatal("unexpected deleted receipt state", statuses, err)
			}
			late := &ServiceReceiptJob{OrderID: purchase.OrderID, Attempt: 1}
			if applied, err := FinishServiceReceipt(ctx, app, late, "retry", now); err != nil || applied {
				t.Fatal("late completion revived deleted receipt", applied, err)
			}
			var exists bool
			if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_purchase_requests WHERE order_id=$1 AND person_id IS NULL)`, purchase.OrderID).Scan(&exists); err != nil || !exists {
				t.Fatal("purchase evidence lost", exists, err)
			}
		})
	}
}
