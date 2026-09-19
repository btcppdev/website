package getters

import (
	"btcpp-web/internal/servicestripe"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
	"sync"
	"testing"
	"time"
)

func TestServiceRefundLedgerPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Refund test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,true)`, person, "refund-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	quote := ServiceQuote{ProductCode: "insider.article", ResourceID: "refund-article", Name: "Article", Provider: "stripe", Quantity: 1, UnitCents: 500, TotalCents: 500, TaxCents: 20, ExpiresAt: time.Now().Add(time.Hour)}
	purchase, err := CreateServicePurchase(app, person, "refund-purchase-test", quote)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := BeginServicePayment(app, person, purchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	reference := "cs_" + attempt.ID
	if err := BindServicePayment(app, attempt.ID, "stripe", reference); err != nil {
		t.Fatal(err)
	}
	paidAt := time.Now().UTC().Truncate(time.Second)
	refund := VerifiedServiceRefund{AttemptID: attempt.ID, Provider: "stripe", PaymentReference: reference, RefundReference: "re_" + attempt.ID, AmountCents: 300, Currency: "USD", RefundedAt: paidAt.Add(time.Second), Reason: "requested_by_customer"}
	pi := &stripe.PaymentIntent{ID: "pi_" + attempt.ID}
	providerRefund := &stripe.Refund{ID: refund.RefundReference, Status: "succeeded", Amount: refund.AmountCents, Currency: "usd", Created: refund.RefundedAt.Unix(), Reason: "requested_by_customer", Charge: &stripe.Charge{ID: "ch_" + attempt.ID}, PaymentIntent: pi}
	client := servicestripe.RefundClient{
		Refund: func(id string, p *stripe.RefundParams) (*stripe.Refund, error) {
			if id != refund.RefundReference {
				t.Error("wrong refund lookup")
			}
			return providerRefund, nil
		},
		Charge: func(id string, p *stripe.ChargeParams) (*stripe.Charge, error) {
			return &stripe.Charge{ID: providerRefund.Charge.ID, Paid: true, Captured: true, Amount: 500, AmountCaptured: 500, AmountRefunded: 300, Currency: "usd", PaymentIntent: pi}, nil
		},
		Checkout: func(id string, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			if id != reference {
				t.Error("did not use saved checkout")
			}
			return &stripe.CheckoutSession{ID: reference, Mode: "payment", Status: "complete", PaymentStatus: "paid", Currency: "usd", AmountTotal: 500, PaymentIntent: pi}, nil
		},
	}
	if err := ReconcileServiceStripeRefund(ctx, app, attempt.ID, refund.RefundReference, servicestripe.RefundClient{}); !errors.Is(err, ErrServiceRefund) {
		t.Fatal("unsettled payment reached provider", err)
	}
	if err := RecordServiceRefund(ctx, app, refund); !errors.Is(err, ErrServiceRefund) {
		t.Fatal("unpaid refund accepted", err)
	}
	payment := VerifiedServicePayment{AttemptID: attempt.ID, Provider: "stripe", Reference: reference, EventID: "evt_" + attempt.ID, Currency: "USD", TotalCents: 500, PaidAt: paidAt}
	if err := RecordServicePayment(app, payment); err != nil {
		t.Fatal(err)
	}
	providerRefund.Status = "pending"
	if err := ReconcileServiceStripeRefund(ctx, app, attempt.ID, refund.RefundReference, client); !errors.Is(err, servicestripe.ErrRefund) {
		t.Fatal("pending provider refund recorded", err)
	}
	var beforeStatus string
	if err := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, purchase.OrderID).Scan(&beforeStatus); err != nil || beforeStatus != "paid" {
		t.Fatal("failed verification changed order", beforeStatus, err)
	}
	providerRefund.Status = "succeeded"
	wrong := refund
	wrong.PaymentReference = "pi_foreign"
	if err := RecordServiceRefund(ctx, app, wrong); !errors.Is(err, ErrServiceRefund) {
		t.Fatal("foreign payment refund accepted", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- ReconcileServiceStripeRefund(ctx, app, attempt.ID, refund.RefundReference, client)
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("duplicate refund failed", err)
		}
	}
	var count, total int64
	if err := app.DB.QueryRow(ctx, `SELECT count(*),sum(amount_cents) FROM service_payment_refunds WHERE attempt_id=$1`, attempt.ID).Scan(&count, &total); err != nil || count != 1 || total != 300 {
		t.Fatal("duplicate refund counted", count, total, err)
	}
	changed := refund
	changed.AmountCents = 301
	if err := RecordServiceRefund(ctx, app, changed); !errors.Is(err, ErrServiceRefund) {
		t.Fatal("refund evidence changed", err)
	}
	// Competing distinct refunds cannot exceed the remaining paid balance.
	results = make(chan error, 2)
	for _, suffix := range []string{"a", "b"} {
		wg.Add(1)
		go func(suffix string) {
			defer wg.Done()
			next := refund
			next.RefundReference += "_" + suffix
			next.AmountCents = 200
			results <- RecordServiceRefund(ctx, app, next)
		}(suffix)
	}
	wg.Wait()
	close(results)
	succeeded, rejected := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, ErrServiceRefund) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatal("concurrent over-refund", succeeded, rejected)
	}
	var status string
	if err := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, purchase.OrderID).Scan(&status); err != nil || status != "refunded" {
		t.Fatal(status, err)
	}
	if err := RecordServicePayment(app, payment); err != nil {
		t.Fatal("settlement replay after refund", err)
	}
	if err := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, purchase.OrderID).Scan(&status); err != nil || status != "refunded" {
		t.Fatal("settlement reopened refund", status, err)
	}
	for _, sql := range []string{`UPDATE service_payment_refunds SET amount_cents=1 WHERE attempt_id=$1`, `DELETE FROM service_payment_refunds WHERE attempt_id=$1`} {
		if _, err := app.DB.Exec(ctx, sql, attempt.ID); err == nil {
			t.Fatal("refund evidence mutation allowed")
		}
	}
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_refund_jobs WHERE attempt_id=$1`, attempt.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("refund delivery not atomically queued", count, err)
	}
	now := time.Now().Add(time.Second)
	if _, err := ClaimInsiderRefundDelivery(ctx, app, attempt.ID, now); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("refund overtook original settlement", err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_fulfillment_jobs SET state='accepted',payload='{"fixture":"settled"}' WHERE attempt_id=$1`, attempt.ID); err != nil {
		t.Fatal(err)
	}
	job, err := ClaimInsiderRefundDelivery(ctx, app, attempt.ID, now)
	if err != nil || job.Revision != 1 || job.Refund.AmountCents != 300 {
		t.Fatal("refund claim", job, err)
	}
	if err := FreezeServiceRefundDelivery(ctx, app, job, []byte(`{"event":"frozen"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimInsiderRefundDelivery(ctx, app, attempt.ID, now); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("refund order bypassed lease", err)
	}
	reclaimed, err := ClaimInsiderRefundDelivery(ctx, app, attempt.ID, now.Add(2*time.Minute))
	if err != nil || reclaimed.Revision != 1 || string(reclaimed.Payload) != string(job.Payload) {
		t.Fatal("refund retry changed payload", reclaimed, err)
	}
	if changed, err := FinishServiceRefundDelivery(ctx, app, job, "accepted", now); err != nil || changed {
		t.Fatal("stale worker accepted refund", changed, err)
	}
	if changed, err := FinishServiceRefundDelivery(ctx, app, reclaimed, "accepted", now); err != nil || !changed {
		t.Fatal(changed, err)
	}
	next, err := ClaimInsiderRefundDelivery(ctx, app, attempt.ID, now.Add(2*time.Minute))
	if err != nil || next.Revision != 2 || next.Refund.AmountCents != 200 {
		t.Fatal("second refund order", next, err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_refund_jobs SET payload='{}' WHERE attempt_id=$1 AND revision=1`, attempt.ID); err == nil {
		t.Fatal("frozen refund payload rewritten")
	}
}
