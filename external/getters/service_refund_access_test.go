package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"errors"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestRefundAccessDecisionPostgres(t *testing.T) {
	for _, finalAction := range []string{"retain", "revoke"} {
		t.Run(finalAction, func(t *testing.T) {
			app := databaseSmokeContext(t)
			ctx := context.Background()
			var person string
			if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Refund allowance test') RETURNING id::text`).Scan(&person); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
					t.Error(err)
				}
			})
			if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "allowance-"+person+"@example.test"); err != nil {
				t.Fatal(err)
			}
			if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
				t.Fatal(err)
			}
			start := time.Now().UTC().Truncate(time.Second)
			end := start.AddDate(1, 0, 0)
			quote := ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Alerts slots", Provider: "stripe", Quantity: 1, UnitCents: 1200, TotalCents: 1200, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(time.Hour)}
			purchase, err := CreateServicePurchase(app, person, "refund-access-purchase", quote)
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
			if err := RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: "stripe", Reference: reference, EventID: "evt_" + attempt.ID, Currency: "USD", TotalCents: 1200, PaidAt: start}); err != nil {
				t.Fatal(err)
			}
			grant, err := FulfillServicePurchase(app, purchase.OrderID)
			if err != nil {
				t.Fatal(err)
			}
			check := func(at time.Time, want int) {
				t.Helper()
				grants, err := ActiveServiceEntitlements(app, person, at)
				if err != nil || len(grants) != want {
					t.Fatal("allowance", grants, err)
				}
				if want == 1 && (grants[0].ID != grant || grants[0].Quantity != 1 || !grants[0].EndsAt.Equal(end)) {
					t.Fatal("retention changed original allowance", grants)
				}
			}
			check(start, 1)
			refund := VerifiedServiceRefund{AttemptID: attempt.ID, Provider: "stripe", PaymentReference: reference, RefundReference: "re_" + attempt.ID, AmountCents: 300, Currency: "USD", RefundedAt: start.Add(time.Second)}
			if err := RecordServiceRefund(ctx, app, refund); err != nil {
				t.Fatal(err)
			}
			check(start, 0)
			saved, err := ReserveAlertsStripeCheckout(app, person, "saved-before-admin-retention", 1, "https://btcpp.dev")
			if err != nil {
				t.Fatal(err)
			}
			before, err := AlertsBillingRevision(app, person)
			if err != nil {
				t.Fatal(err)
			}
			// Admin retention races with a provider response. Its reference must be
			// saved for reconciliation, but no overlapping payment URL may escape.
			sessionID := "cs_test_" + saved.Intent.ID
			client := alertsstripe.Client{Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
				if e := RecordServiceRefundAccessDecision(ctx, app, person, "stripe", refund.RefundReference, "retain", "Keep paid allowance after exceptional refund"); e != nil {
					t.Fatal(e)
				}
				return &stripe.CheckoutSession{ID: sessionID, Status: "open", Mode: "subscription", ExpiresAt: time.Now().Add(time.Hour).Unix(), ClientReferenceID: saved.Intent.ID, Metadata: map[string]string{"product": "alerts.repository-slots", "account_id": person, "checkout_intent": saved.Intent.ID}, URL: "https://checkout.stripe.com/c/pay/" + sessionID}, nil
			}}
			if destination, e := OpenAlertsStripeCheckout(ctx, app, person, saved.Intent.ID, client); !errors.Is(e, ErrAlertsStripeCheckoutConflict) || destination != "" {
				t.Fatal("concurrent retained access exposed payment link", destination, e)
			}
			stored, e := AlertsStripeCheckoutForPerson(app, person, saved.Intent.ID)
			if e != nil || stored.Reference != sessionID {
				t.Fatal("provider reference lost after conflict", stored, e)
			}
			client.Retrieve = func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
				t.Fatal("saved checkout bypassed retained access check")
				return nil, nil
			}
			if destination, e := OpenAlertsStripeCheckout(ctx, app, person, saved.Intent.ID, client); !errors.Is(e, ErrAlertsStripeCheckoutConflict) || destination != "" {
				t.Fatal("retained access resumed saved checkout", destination, e)
			}
			if err := RecordServiceRefundAccessDecision(ctx, app, person, "stripe", refund.RefundReference, "retain", "Keep paid allowance after exceptional refund"); err != nil {
				t.Fatal(err)
			}
			inspected, e := ListAlertsCheckouts(ctx, app, saved.Intent.ID)
			if e != nil || len(inspected) != 1 || !inspected[0].AccessConflict || inspected[0].State != "referenced" || inspected[0].SessionReference != sessionID {
				t.Fatal("operator inspection hid retained access conflict", inspected, e)
			}
			session := &stripe.CheckoutSession{ID: sessionID, Status: "open", Mode: "subscription", PaymentStatus: "unpaid", ClientReferenceID: saved.Intent.ID, Metadata: map[string]string{"product": "alerts.repository-slots", "account_id": person, "checkout_intent": saved.Intent.ID}}
			client.Retrieve = func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }
			expirationCalls := 0
			client.Expire = func(string, *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
				expirationCalls++
				session.Status = "expired"
				return nil, errors.New("lost expiration acknowledgement")
			}
			if handled, e := expireConflictingAlertsCheckout(ctx, app, person, saved.Intent.ID, client); !handled || e == nil {
				t.Fatal("ambiguous expiration accepted", handled, e)
			}
			stored, e = AlertsStripeCheckoutForPerson(app, person, saved.Intent.ID)
			if e != nil || stored.Closed {
				t.Fatal("unconfirmed expiration closed locally", stored, e)
			}
			if handled, e := expireConflictingAlertsCheckout(ctx, app, person, saved.Intent.ID, client); !handled || e != nil {
				t.Fatal("expiration recovery", handled, e)
			}
			stored, e = AlertsStripeCheckoutForPerson(app, person, saved.Intent.ID)
			if e != nil || !stored.Closed || expirationCalls != 1 {
				t.Fatal("expiration failed to close or replayed provider mutation", stored, e, expirationCalls)
			}
			after, err := AlertsBillingRevision(app, person)
			if err != nil || before == after {
				t.Fatal("access decision did not invalidate old billing review", err)
			}
			check(start, 1)
			check(end, 0)
			state, err := LoadAlertsBillingState(app, person)
			if err != nil || len(state.Orders) != 1 || !state.Orders[0].RefundRetained {
				t.Fatalf("retained refund missing from pricing evidence: %+v %v", state, err)
			}
			if _, err := ReserveAlertsStripeCheckout(app, person, "retained-refund-overlap", 1, "https://btcpp.dev"); !errors.Is(err, ErrAlertsStripeCheckoutConflict) {
				t.Fatal("retained access allowed overlapping subscription", err)
			}

			refund.RefundReference += "_remaining"
			refund.AmountCents = 900
			if err := RecordServiceRefund(ctx, app, refund); err != nil {
				t.Fatal(err)
			}
			check(start, 0)
			if err := RecordServiceRefundAccessDecision(ctx, app, person, "stripe", refund.RefundReference, finalAction, "Explicit final refund access decision"); err != nil {
				t.Fatal(err)
			}
			expected := 0
			if finalAction == "retain" {
				expected = 1
			}
			check(start, expected)
			if finalAction == "retain" {
				if _, err := ReserveAlertsStripeCheckout(app, person, "full-refund-overlap", 1, "https://btcpp.dev"); !errors.Is(err, ErrAlertsStripeCheckoutConflict) {
					t.Fatal("fully refunded retained access allowed overlapping subscription", err)
				}
			}

			if _, err := app.DB.Exec(ctx, `UPDATE service_entitlements SET revoked_at=now(),revocation_actor='admin test',revocation_reason='Separate revocation' WHERE id=$1`, grant); err != nil {
				t.Fatal(err)
			}
			check(start, 0)
			if _, err := ReserveAlertsStripeCheckout(app, person, "after-access-revocation", 1, "https://btcpp.dev"); err != nil {
				t.Fatal("revoked access blocked a replacement subscription", err)
			}
		})
	}
}

func TestDeletedAdminCannotDecideRefundAccessPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var actor string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name,is_deleted_account) VALUES('Deleted admin fixture',false) RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE people SET is_deleted_account=true WHERE id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if err := RecordServiceRefundAccessDecision(ctx, app, actor, "opennode", "unused-reference", "retain", "reason"); !errors.Is(err, ErrServiceRefundAdmin) {
		t.Fatal("deleted admin reached refund lookup", err)
	}
}
