package getters

import (
	"context"
	"errors"
	"testing"

	"btcpp-web/internal/alertsstripe"
	"github.com/stripe/stripe-go/v86"
)

func TestAlertsStripeCancelPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Cancel fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	stored, err := ReserveAlertsStripeCheckout(app, person, "cancel-fixture-unsubmitted", 1, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, alertsstripe.Client{}); err != nil {
		t.Fatal(err)
	}
	stored, err = ReserveAlertsStripeCheckout(app, person, "cancel-fixture-submitted", 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	ref := "cs_" + stored.Intent.ID
	if _, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$2,provider_started_at=now() WHERE id=$1`, stored.Intent.ID, ref); err != nil {
		t.Fatal(err)
	}
	session := &stripe.CheckoutSession{ID: ref, Mode: "subscription", Status: "open", PaymentStatus: "unpaid", ClientReferenceID: stored.Intent.ID, Metadata: map[string]string{"account_id": person, "checkout_intent": stored.Intent.ID, "product": "alerts.repository-slots"}}
	expires := 0
	client := alertsstripe.Client{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Expire: func(string, *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
		expires++
		return nil, errors.New("timeout")
	}}
	if err := CancelAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, client); err == nil {
		t.Fatal("timeout released checkout")
	}
	saved, err := AlertsStripeCheckoutForPerson(app, person, stored.Intent.ID)
	if err != nil || saved.Closed {
		t.Fatal("ambiguous cancellation closed checkout", err)
	}
	session.Status = "complete"
	session.PaymentStatus = "paid"
	if err := CancelAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, client); err == nil {
		t.Fatal("completed payment canceled")
	}
	if expires != 1 {
		t.Fatal("completed payment sent to expire", expires)
	}
	// Recover a successful expiration whose response was lost.
	session.Status = "expired"
	session.PaymentStatus = "unpaid"
	if err := CancelAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, client); err != nil {
		t.Fatal(err)
	}
	if expires != 1 {
		t.Fatal("expired session expired twice")
	}
}
