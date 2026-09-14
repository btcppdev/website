package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestServicePaymentsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	create := func(quotes ...ServiceQuote) (string, *ServicePurchase) {
		t.Helper()
		var person string
		if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Service payment test') RETURNING id::text`).Scan(&person); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
				t.Error(err)
			}
		})
		if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,true)`, person, "pay-"+person+"@example.test"); err != nil {
			t.Fatal(err)
		}
		quote := ServiceQuote{ProductCode: "insider.article", ResourceID: "article-test", Name: "Article access", Provider: "stripe", Quantity: 1, UnitCents: 500, TotalCents: 500, TaxCents: 20, ExpiresAt: time.Now().Add(time.Hour)}
		if len(quotes) > 0 {
			quote = quotes[0]
		}
		purchase, err := CreateServicePurchase(app, person, "payment-purchase-01", quote)
		if err != nil {
			t.Fatal(err)
		}
		return person, purchase
	}
	person, purchase := create()
	const n = 6
	results := make(chan *ServicePaymentAttempt, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := BeginServicePayment(app, person, purchase.OrderID)
			results <- a
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
	var attempt *ServicePaymentAttempt
	for a := range results {
		if attempt != nil && a.ID != attempt.ID {
			t.Fatal("duplicate payment attempts")
		}
		attempt = a
	}
	if attempt.TotalCents != 500 || attempt.Provider != "stripe" || attempt.Reference != "" || !attempt.CanCreateCharge {
		t.Fatalf("attempt=%+v", attempt)
	}
	other, otherPurchase := create()
	if _, err := BeginServicePayment(app, other, purchase.OrderID); !errors.Is(err, ErrServicePayment) {
		t.Fatalf("cross-account attempt: %v", err)
	}
	ref := "test-charge-" + attempt.ID
	for i := 0; i < 2; i++ {
		if err := BindServicePayment(app, attempt.ID, "stripe", ref); err != nil {
			t.Fatal(err)
		}
	}
	if err := BindServicePayment(app, attempt.ID, "stripe", ref+"changed"); !errors.Is(err, ErrServicePayment) {
		t.Fatalf("rebind: %v", err)
	}
	payment := VerifiedServicePayment{AttemptID: attempt.ID, Provider: "stripe", Reference: ref, EventID: "test-event-" + attempt.ID, Currency: "USD", TotalCents: 500, PaidAt: time.Now().UTC().Truncate(time.Microsecond)}
	for _, field := range []string{"amount", "currency", "reference", "provider"} {
		bad := payment
		switch field {
		case "amount":
			bad.TotalCents--
		case "currency":
			bad.Currency = "EUR"
		case "reference":
			bad.Reference += "bad"
		case "provider":
			bad.Provider = "opennode"
		}
		if err := RecordServicePayment(app, bad); !errors.Is(err, ErrServicePayment) {
			t.Fatalf("%s mismatch: %v", field, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := RecordServicePayment(app, payment); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	var events, fulfilled int
	if err := app.DB.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM service_payment_events WHERE attempt_id=$2),
		(SELECT sum(fulfilled_quantity) FROM shop_order_items WHERE order_id=$1) FROM shop_orders WHERE id=$1`, purchase.OrderID, attempt.ID).Scan(&status, &events, &fulfilled); err != nil {
		t.Fatal(err)
	}
	if status != "paid" || events != 1 || fulfilled != 0 {
		t.Fatalf("status=%s events=%d fulfilled=%d", status, events, fulfilled)
	}
	retry, err := BeginServicePayment(app, person, purchase.OrderID)
	if err != nil || retry.ID != attempt.ID || retry.CanCreateCharge {
		t.Fatalf("settled attempt retry: %+v %v", retry, err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE shop_orders SET status='refunded' WHERE id=$1`, purchase.OrderID); err != nil {
		t.Fatal(err)
	}
	if err := RecordServicePayment(app, payment); err != nil {
		t.Fatal(err)
	}
	if err := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, purchase.OrderID).Scan(&status); err != nil || status != "refunded" {
		t.Fatalf("replay overwrote refund: %s %v", status, err)
	}
	changed := payment
	changed.PaidAt = changed.PaidAt.Add(time.Second)
	if err := RecordServicePayment(app, changed); !errors.Is(err, ErrServicePayment) {
		t.Fatalf("changed event: %v", err)
	}
	second, err := BeginServicePayment(app, other, otherPurchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if err := BindServicePayment(app, second.ID, "stripe", ref); err == nil {
		t.Fatal("charge bound to two purchases")
	}
	ref2 := "test-charge-" + second.ID
	if err := BindServicePayment(app, second.ID, "stripe", ref2); err != nil {
		t.Fatal(err)
	}
	p2 := payment
	p2.AttemptID = second.ID
	p2.Reference = ref2
	if err := RecordServicePayment(app, p2); !errors.Is(err, ErrServicePayment) {
		t.Fatalf("event reused for other purchase: %v", err)
	}
	p2.EventID = "test-event-" + second.ID
	if _, err := app.DB.Exec(ctx, `UPDATE shop_orders SET status='cancelled' WHERE id=$1`, otherPurchase.OrderID); err != nil {
		t.Fatal(err)
	}
	if err := RecordServicePayment(app, p2); err != nil {
		t.Fatal(err)
	}
	var settled bool
	if err := app.DB.QueryRow(ctx, `SELECT o.status,a.settled_at IS NOT NULL FROM shop_orders o CROSS JOIN service_payment_attempts a WHERE o.id=$1 AND a.id=$2`, otherPurchase.OrderID, second.ID).Scan(&status, &settled); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || !settled {
		t.Fatalf("late payment: %s settled=%v", status, settled)
	}
	for _, sql := range []string{`UPDATE service_payment_attempts SET provider_reference='changed' WHERE id=$1`, `UPDATE service_payment_attempts SET settled_at=NULL WHERE id=$1`, `DELETE FROM service_payment_events WHERE attempt_id=$1`} {
		if _, err := app.DB.Exec(ctx, sql, attempt.ID); err == nil {
			t.Fatalf("financial evidence mutation: %s", sql)
		}
	}
	thirdPerson, thirdPurchase := create()
	third, err := BeginServicePayment(app, thirdPerson, thirdPurchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	ref3 := "test-charge-" + third.ID
	if err := BindServicePayment(app, third.ID, "stripe", ref3); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE shop_orders SET total_cents=1 WHERE id=$1`, thirdPurchase.OrderID); err != nil {
		t.Fatal(err)
	}
	p3 := payment
	p3.AttemptID = third.ID
	p3.Reference = ref3
	p3.EventID = "test-event-" + third.ID
	if err := RecordServicePayment(app, p3); err != nil {
		t.Fatal(err)
	}
	if err := app.DB.QueryRow(ctx, `SELECT o.status,a.settled_at IS NOT NULL FROM shop_orders o CROSS JOIN service_payment_attempts a WHERE o.id=$1 AND a.id=$2`, thirdPurchase.OrderID, third.ID).Scan(&status, &settled); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || !settled {
		t.Fatalf("edited order: %s settled=%v", status, settled)
	}
	// Review is observational: discover unresolved money without changing state.
	fourthPerson, fourthPurchase := create()
	fourth, err := BeginServicePayment(app, fourthPerson, fourthPurchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	checkReview := func(asOf time.Time, wantFourth bool) {
		t.Helper()
		found := map[string]ServicePaymentReview{}
		after := ""
		for {
			page, err := ReviewServicePayments(app, after, asOf)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) > 100 {
				t.Fatal("unbounded review page")
			}
			for _, item := range page.Items {
				if _, ok := found[item.AttemptID]; ok {
					t.Fatal("repeated review row")
				}
				found[item.AttemptID] = item
			}
			if page.Next == "" {
				break
			}
			if page.Next <= after {
				t.Fatal("review cursor did not advance")
			}
			after = page.Next
		}
		if _, ok := found[attempt.ID]; ok {
			t.Fatal("refunded order needs no payment replay")
		}
		if found[second.ID].Reason != "payment_after_cancellation" {
			t.Fatalf("cancelled review: %+v", found[second.ID])
		}
		if got := found[third.ID]; got.Reason != "order_amount_mismatch" || got.ExpectedCents != 500 || got.OrderCents != 1 {
			t.Fatalf("edited review: %+v", got)
		}
		got, ok := found[fourth.ID]
		if ok != wantFourth || ok && got.Reason != "payment_unresolved" {
			t.Fatalf("stale attempt review: %+v present=%v", got, ok)
		}
	}
	checkReview(time.Now(), false)
	checkReview(time.Now().Add(16*time.Minute), true)
	start := time.Now().UTC().Truncate(time.Second)
	end := start.AddDate(1, 0, 0)
	serviceQuote := ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Alerts slots", Provider: "stripe", Quantity: 1, UnitCents: 1200, TotalCents: 1200, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(time.Hour)}
	for _, mutation := range []string{
		"",
		`UPDATE shop_order_items SET quantity=2 WHERE order_id=$1`,
		`UPDATE shop_order_items SET unit_price_cents=1 WHERE order_id=$1`,
		`UPDATE shop_order_items SET service_product_code='insider.membership' WHERE order_id=$1`,
		`UPDATE shop_order_items SET service_resource_id='other-resource' WHERE order_id=$1`,
		`UPDATE shop_order_items SET service_period_end=service_period_end+interval '1 day' WHERE order_id=$1`,
		`DELETE FROM shop_order_items WHERE order_id=$1`,
	} {
		buyer, order := create(serviceQuote)
		a, err := BeginServicePayment(app, buyer, order.OrderID)
		if err != nil {
			t.Fatal(err)
		}
		reference := "test-charge-" + a.ID
		if err := BindServicePayment(app, a.ID, "stripe", reference); err != nil {
			t.Fatal(err)
		}
		if mutation != "" {
			if _, err := app.DB.Exec(ctx, mutation, order.OrderID); err != nil {
				t.Fatal(err)
			}
		}
		verified := VerifiedServicePayment{AttemptID: a.ID, Provider: "stripe", Reference: reference, EventID: "test-event-" + a.ID, Currency: "USD", TotalCents: 1200, PaidAt: start}
		if err := RecordServicePayment(app, verified); err != nil {
			t.Fatal(err)
		}
		wantStatus, wantReason := "pending", "order_item_mismatch"
		if mutation == "" {
			wantStatus, wantReason = "paid", "paid_order_unfulfilled"
		}
		if err := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, order.OrderID).Scan(&status); err != nil || status != wantStatus {
			t.Fatalf("item settlement status: %s want %s err=%v", status, wantStatus, err)
		}
		after := ""
		found := false
		for {
			page, err := ReviewServicePayments(app, after, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range page.Items {
				if item.AttemptID == a.ID {
					found = true
					if item.Reason != wantReason {
						t.Fatalf("item review reason: %s", item.Reason)
					}
				}
			}
			if page.Next == "" {
				break
			}
			after = page.Next
		}
		if !found {
			t.Fatal("changed item missing from reconciliation")
		}
	}
}
