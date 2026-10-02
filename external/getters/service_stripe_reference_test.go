package getters

import (
	"context"
	"errors"
	"testing"

	"btcpp-web/internal/servicestripe"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestServiceLostCheckoutReferencePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	person := uuid.NewString()
	if _, err := app.DB.Exec(ctx, `INSERT INTO people(id,name) VALUES($1,'Insider lost checkout fixture')`, person); err != nil {
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
	product := "insider.membership.monthly"
	terms := servicestripe.CheckoutIntent{ProductCode: product, Quantity: 1, TaxCode: "txcd_test", ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/success", CancelURL: "https://insider.btcpp.dev/cancel"}
	stored, err := ReserveServiceStripeCheckout(app, person, uuid.NewString(), terms)
	if err != nil {
		t.Fatal(err)
	}
	if err := markServiceStripeCheckoutStarted(ctx, app, person, stored.Intent.ID, product); err != nil {
		t.Fatal(err)
	}
	calls := 0
	found := false
	session := &stripe.CheckoutSession{ID: "cs_lost_" + stored.Intent.ID, Mode: "subscription", Status: "open", PaymentStatus: "unpaid", ClientReferenceID: stored.Intent.ID, Created: stored.Intent.CreatedAt.Unix(), Metadata: map[string]string{"account_id": person, "checkout_intent": stored.Intent.ID, "product": product}}
	client := servicestripe.CheckoutClient{
		Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			t.Fatal("recovery created a checkout")
			return nil, nil
		},
		List: func(*stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
			calls++
			if !found {
				return &stripe.CheckoutSessionList{}, nil
			}
			return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{session}}, nil
		},
		Retrieve: func(id string, _ *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			if id != session.ID {
				t.Fatal("foreign reference", id)
			}
			return session, nil
		},
	}
	recover := func() error {
		return RecoverServiceStripeCheckout(ctx, app, person, stored.Intent.ID, product, client, servicestripe.InvoiceClient{})
	}
	if err := recover(); !errors.Is(err, servicestripe.ErrReconcile) {
		t.Fatal("missing candidate treated as terminal", err)
	}
	current, err := ServiceStripeCheckoutForPerson(app, person, stored.Intent.ID, product)
	if err != nil || current.Closed || current.Reference != "" {
		t.Fatal("absence changed reservation", current, err)
	}
	found = true
	if err := recover(); err != nil {
		t.Fatal(err)
	}
	if err := recover(); err != nil {
		t.Fatal(err)
	}
	current, err = ServiceStripeCheckoutForPerson(app, person, stored.Intent.ID, product)
	if err != nil || current.Reference != session.ID || calls != 2 {
		t.Fatal("reference not durable", current, calls, err)
	}
	if err := RecoverServiceStripeCheckout(ctx, app, uuid.NewString(), stored.Intent.ID, product, client, servicestripe.InvoiceClient{}); err == nil {
		t.Fatal("foreign owner recovered")
	}
	if err := RecoverServiceStripeCheckout(ctx, app, person, stored.Intent.ID, "insider.membership.annual", client, servicestripe.InvoiceClient{}); err == nil {
		t.Fatal("foreign product recovered")
	}
	session.Status = "expired"
	if err := recover(); err != nil {
		t.Fatal(err)
	}
	other, err := ReserveServiceStripeCheckout(app, person, uuid.NewString(), terms)
	if err != nil {
		t.Fatal(err)
	}
	session.ID = "cs_discovered_" + other.Intent.ID
	session.Status = "open"
	session.ClientReferenceID = other.Intent.ID
	session.Created = other.Intent.CreatedAt.Unix()
	session.Metadata["checkout_intent"] = other.Intent.ID
	competing := "cs_concurrent_" + other.Intent.ID
	client.List = func(*stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
		if _, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET stripe_session_id=$2 WHERE id=$1`, other.Intent.ID, competing); err != nil {
			t.Fatal(err)
		}
		return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{session}}, nil
	}
	if _, err := recoverServiceStripeCheckoutReference(ctx, app, person, other.Intent.ID, product, client); !errors.Is(err, ErrServiceStripeCheckoutConflict) {
		t.Fatal("conflicting response accepted", err)
	}
	current, err = ServiceStripeCheckoutForPerson(app, person, other.Intent.ID, product)
	if err != nil || current.Reference != competing {
		t.Fatal("concurrent reference overwritten", current, err)
	}
}
