package getters

import (
	"btcpp-web/internal/servicestripe"
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestServiceRefundMissedWebhookPostgres(t *testing.T) {
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
	purchase, err := CreateServicePurchase(app, person, "refund-worker-test", quote)
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

	payment := VerifiedServicePayment{AttemptID: attempt.ID, Provider: "stripe", Reference: reference, EventID: "evt_" + attempt.ID, Currency: "USD", TotalCents: 500, PaidAt: paidAt}
	if err := RecordServicePayment(app, payment); err != nil {
		t.Fatal(err)
	}
	sc := servicestripe.RefundSyncClient{Verify: client, RefundIDs: func(ctx context.Context, p, c string) ([]string, error) {
		if p == pi.ID {
			return []string{refund.RefundReference}, nil
		}
		return nil, nil
	}}
	savedCheckout := sc.Verify.Checkout
	sc.Verify.Checkout = func(id string, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		if id == reference {
			return savedCheckout(id, p)
		}
		return &stripe.CheckoutSession{ID: id, PaymentIntent: &stripe.PaymentIntent{ID: "pi_unrelated"}}, nil
	}
	sc.Verify.Invoice = func(id string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
		return &stripe.Invoice{ID: id, Payments: &stripe.InvoicePaymentList{Data: []*stripe.InvoicePayment{{Payment: &stripe.InvoicePaymentPayment{Type: "payment_intent", PaymentIntent: &stripe.PaymentIntent{ID: "pi_unrelated"}}}}}}, nil
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO service_stripe_refund_checks(attempt_id,next_check_at) VALUES($1,'1970-01-01')`, attempt.ID); err != nil {
		t.Fatal(err)
	}
	providerRefund.Status = "pending"
	if _, err := ReconcileDueServiceStripeRefunds(ctx, app, sc); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1`, attempt.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	providerRefund.Status = "succeeded"
	if _, err := app.DB.Exec(ctx, `UPDATE service_stripe_refund_checks SET next_check_at='1970-01-01' WHERE attempt_id=$1`, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileDueServiceStripeRefunds(ctx, app, sc); err != nil {
		t.Fatal(err)
	}
	summary, err := ServiceOrderRefunds(ctx, app, purchase.OrderID)
	if err != nil || summary.RefundedCents != 300 {
		t.Fatal(summary, err)
	}
	var checked bool
	if err := app.DB.QueryRow(ctx, `SELECT last_success_at IS NOT NULL AND next_check_at>now() AND failures=0 FROM service_stripe_refund_checks WHERE attempt_id=$1`, attempt.ID).Scan(&checked); err != nil || !checked {
		t.Fatal("not scheduled", checked, err)
	}
}
