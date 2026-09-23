package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"testing"
)

func TestAlertsLostCheckoutReferencePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	person := uuid.NewString()
	if _, err := app.DB.Exec(ctx, `INSERT INTO people(id,name) VALUES($1,'Lost checkout response fixture')`, person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	stored, err := ReserveAlertsStripeCheckout(app, person, uuid.NewString(), 1, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	inspected, e := ListAlertsCheckouts(ctx, app, stored.Intent.ID)
	if e != nil || len(inspected) != 1 || inspected[0].State != "missing_reference" || inspected[0].AccessConflict {
		t.Fatal("missing-reference inspection", inspected, e)
	}
	encoded, e := json.Marshal(inspected)
	if e != nil || strings.Contains(string(encoded), "@example.test") || strings.Contains(string(encoded), "Email") {
		t.Fatal("buyer details exposed", e)
	}
	if _, e := ListAlertsCheckouts(ctx, nil, "not-a-uuid"); e == nil {
		t.Fatal("invalid inspection key accepted")
	}
	calls := 0
	found := false
	session := &stripe.CheckoutSession{ID: "cs_lost_" + stored.Intent.ID, Mode: "subscription", Status: "open", PaymentStatus: "unpaid", ClientReferenceID: stored.Intent.ID, Created: stored.Intent.CreatedAt.Unix(), Metadata: map[string]string{"account_id": person, "checkout_intent": stored.Intent.ID, "product": "alerts.repository-slots"}}
	client := alertsstripe.Client{
		Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			t.Fatal("recovery created a checkout")
			return nil, nil
		},
		ListCheckouts: func(*stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
			calls++
			if !found {
				return &stripe.CheckoutSessionList{}, nil
			}
			return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{session}}, nil
		},
		Retrieve: func(id string, _ *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			if id != session.ID {
				t.Fatal(id)
			}
			return session, nil
		},
	}
	if err = RecoverAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, client); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("no match treated as terminal", err)
	}
	current, err := AlertsStripeCheckoutForPerson(app, person, stored.Intent.ID)
	if err != nil || current.Closed || current.Reference != "" {
		t.Fatal("unconfirmed absence changed checkout", current, err)
	}
	found = true
	if err = RecoverAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, client); err != nil {
		t.Fatal(err)
	}
	// A new getter call models process restart: recovery uses the durable reference.
	if err = RecoverAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, client); err != nil {
		t.Fatal(err)
	}
	current, err = AlertsStripeCheckoutForPerson(app, person, stored.Intent.ID)
	if err != nil || current.Reference != session.ID || calls != 2 {
		t.Fatal("reference was not durable", current, calls, err)
	}
	if err = RecoverAlertsStripeCheckout(ctx, app, uuid.NewString(), stored.Intent.ID, client); err == nil {
		t.Fatal("foreign account recovered checkout")
	}
	// Free the verified expired reservation, then race discovery of a new
	// intent against a different reference committed by a concurrent response.
	session.Status = "expired"
	if err = CloseExpiredAlertsStripeCheckout(ctx, app, person, stored.Intent.ID, session.ID, client); err != nil {
		t.Fatal(err)
	}
	inspected, e = ListAlertsCheckouts(ctx, app, stored.Intent.ID)
	if e != nil || len(inspected) != 1 || inspected[0].State != "closed" || inspected[0].ClosedAt == nil {
		t.Fatal("closed inspection", inspected, e)
	}
	pending, e := ListAlertsCheckouts(ctx, app, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(pending) > 100 {
		t.Fatal("unbounded operator query")
	}
	for _, item := range pending {
		if item.CheckoutID == stored.Intent.ID {
			t.Fatal("closed checkout listed as unresolved")
		}
	}
	other, err := ReserveAlertsStripeCheckout(app, person, uuid.NewString(), 1, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	session.ID = "cs_discovered_" + other.Intent.ID
	session.Status = "open"
	session.ClientReferenceID = other.Intent.ID
	session.Created = other.Intent.CreatedAt.Unix()
	session.Metadata["checkout_intent"] = other.Intent.ID
	competing := "cs_concurrent_" + other.Intent.ID
	client.ListCheckouts = func(*stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
		if _, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$2 WHERE id=$1`, other.Intent.ID, competing); err != nil {
			t.Fatal(err)
		}
		return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{session}}, nil
	}
	if _, err = recoverAlertsStripeCheckoutReference(ctx, app, person, other.Intent.ID, client); !errors.Is(err, ErrAlertsStripeCheckoutConflict) {
		t.Fatal("conflicting discovered reference accepted", err)
	}
	current, err = AlertsStripeCheckoutForPerson(app, person, other.Intent.ID)
	if err != nil || current.Reference != competing {
		t.Fatal("concurrent reference overwritten", current, err)
	}
}
