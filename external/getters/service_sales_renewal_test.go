package getters

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"github.com/stripe/stripe-go/v86"
)

func assertClosureRenewal(t *testing.T, app *config.AppContext, person, checkout, product string, sub *stripe.Subscription, update *func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error), refresh func() error, process func(string) (string, error)) {
	t.Helper()
	ctx := context.Background()
	service := "insider"
	if product == "alerts.repository-slots" {
		service = "alerts"
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin') ON CONFLICT DO NOTHING`, person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `TRUNCATE service_closure_plans,service_sales_closures`); err != nil {
			t.Error(err)
		}
	})
	sub.CancelAtPeriodEnd = false
	sub.CancelAt = 0
	calls := 0
	lost := errors.New("response lost after scheduling closure cancellation")
	*update = func(id string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
		calls++
		if id != sub.ID || p.CancelAtPeriodEnd == nil || !*p.CancelAtPeriodEnd || p.IdempotencyKey == nil {
			t.Fatal("unexpected closure update")
		}
		var pending int
		if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_stripe_cancellations WHERE checkout_id=$1 AND state='pending'`, checkout).Scan(&pending); err != nil || pending != 1 {
			t.Fatal("missing durable cancellation", pending, err)
		}
		sub.CancelAtPeriodEnd = true
		return nil, lost
	}
	run := func() error {
		return cancelStoppedServiceRenewal(ctx, app, person, checkout, product, refresh, process)
	}
	if err := run(); err != nil || calls != 0 {
		t.Fatal("active service modified", err, calls)
	}
	at := time.Now().UTC()
	before, err := ActiveServiceEntitlements(app, person, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := StopServiceSales(ctx, app, person, service, "Permanent closure"); err != nil {
		t.Fatal(err)
	}
	if err := run(); !errors.Is(err, lost) {
		t.Fatal("lost provider response", err)
	}
	if err := run(); err != nil {
		t.Fatal("closure retry", err)
	}
	if err := run(); err != nil || calls != 1 {
		t.Fatal("repeated provider mutation", err, calls)
	}
	after, err := ActiveServiceEntitlements(app, person, at)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("paid access changed", before, after, err)
	}
	var pending int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_stripe_cancellations WHERE checkout_id=$1 AND state='pending'`, checkout).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("unconfirmed retry", pending, err)
	}
	// Someone reversing cancellation at the provider cannot make the old success
	// look current or trigger endless mutations with an old idempotency key.
	sub.CancelAtPeriodEnd = false
	if err := run(); !errors.Is(err, ErrServiceClosureCancellationReview) || calls != 1 {
		t.Fatal("reactivation not flagged", err, calls)
	}
	// Terminal provider status means no renewals remain, even when the access
	// classification preserves a failed-payment or dispute reason.
	for _, reason := range []string{"payment_failed", "payment_disputed", "cancellation_requested"} {
		sub.Status = stripe.SubscriptionStatusCanceled
		sub.CancellationDetails = &stripe.SubscriptionCancellationDetails{Reason: stripe.SubscriptionCancellationDetailsReason(reason)}
		if err := run(); err != nil || calls != 1 {
			t.Fatal("terminal subscription cancellation", reason, err, calls)
		}
	}
	sub.Status = stripe.SubscriptionStatusIncompleteExpired
	sub.CancellationDetails = nil
	if err := run(); err != nil || calls != 1 {
		t.Fatal("expired initial subscription", err, calls)
	}
}
