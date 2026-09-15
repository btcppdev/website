package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestAlertsStripeExpiredReservationPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Expired checkout fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	}()
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	stored, err := ReserveAlertsStripeCheckout(app, person, "expiry-fixture-key-001", 1, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	session := &stripe.CheckoutSession{ID: "cs_" + stored.Intent.ID, Mode: "subscription", Status: "open", PaymentStatus: "unpaid", ClientReferenceID: stored.Intent.ID, Metadata: map[string]string{"account_id": person, "checkout_intent": stored.Intent.ID, "product": "alerts.repository-slots"}}
	client := alertsstripe.Client{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }}
	if err = CloseExpiredAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, session.ID, client); err == nil {
		t.Fatal("open checkout closed")
	}
	if _, err = ReserveAlertsStripeCheckout(app, person, "expiry-fixture-key-002", 2, "https://btcpp.dev"); err == nil {
		t.Fatal("active reservation released")
	}
	session.Status = "expired"
	for i := 0; i < 2; i++ {
		if err = CloseExpiredAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, session.ID, client); err != nil {
			t.Fatal(err)
		}
	}
	closed, err := AlertsStripeCheckoutForPerson(app, person, stored.Intent.ID)
	if err != nil || !closed.Closed || closed.Reference != session.ID {
		t.Fatal(closed, err)
	}
	next, err := ReserveAlertsStripeCheckout(app, person, "expiry-fixture-key-002", 2, "https://btcpp.dev")
	if err != nil || next.Intent.ID == stored.Intent.ID {
		t.Fatal(next, err)
	}
	// Replaying the old expiration must not close the replacement reservation.
	if err = CloseExpiredAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, session.ID, client); err != nil {
		t.Fatal(err)
	}
	next, err = AlertsStripeCheckoutForPerson(app, person, next.Intent.ID)
	if err != nil || next.Closed {
		t.Fatal(next, err)
	}
}
