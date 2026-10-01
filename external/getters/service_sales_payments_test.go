package getters

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/servicestripe"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestStoppedOneOffCheckoutPostgres(t *testing.T) {
	for _, scenario := range []string{"expire", "paid", "missing expire", "missing paid"} {
		t.Run(scenario, func(t *testing.T) {
			paid := strings.Contains(scenario, "paid")
			missing := strings.HasPrefix(scenario, "missing")
			ctx := context.Background()
			app := databaseSmokeContext(t)
			var person string
			if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('One-off closure fixture') RETURNING id::text`).Scan(&person); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := app.DB.Exec(ctx, `TRUNCATE service_closure_plans,service_sales_closures`); err != nil {
					t.Error(err)
				}
			})
			if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
				t.Fatal(err)
			}
			if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
				t.Fatal(err)
			}
			purchase, err := CreateServicePurchase(app, person, uuid.NewString(), ServiceQuote{ProductCode: "insider.article", ResourceID: uuid.NewString(), Name: "Article", Provider: "stripe", Quantity: 1, UnitCents: 200, TotalCents: 200, ExpiresAt: time.Now().Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := BeginServicePayment(app, person, purchase.OrderID)
			if err != nil {
				t.Fatal(err)
			}
			reference := "cs_" + attempt.ID
			if !missing {
				if err := BindServicePayment(app, attempt.ID, "stripe", reference); err != nil {
					t.Fatal(err)
				}
			}
			session := &stripe.CheckoutSession{Created: time.Now().Unix(), ID: reference, Mode: "payment", Status: "open", PaymentStatus: "unpaid", Currency: "usd", AmountTotal: 200, Metadata: map[string]string{"checkout-kind": "service", "service-attempt-id": attempt.ID, "service-order-id": purchase.OrderID}}
			calls := 0
			client := servicestripe.CheckoutClient{List: func(*stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
				return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{session}}, nil
			}, Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Expire: func(string, *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
				calls++
				session.Status = "expired"
				return nil, errors.New("lost expiration response")
			}}
			if err := expireStoppedServicePaymentCheckout(ctx, app, attempt.ID, client); err == nil || calls != 0 {
				t.Fatal("active sales modified", err, calls)
			}
			if err := StopServiceSales(ctx, app, person, "insider", "Permanent closure"); err != nil {
				t.Fatal(err)
			}
			if paid {
				session.Status = "complete"
				session.PaymentStatus = "paid"
			}
			if missing {
				if _, err := app.DB.Exec(ctx, `UPDATE service_payment_attempts SET next_reconcile_at='2000-01-01' WHERE id=$1`, attempt.ID); err != nil {
					t.Fatal(err)
				}
				if count, err := ReconcileStoppedServicePaymentCheckouts(ctx, app, client); err == nil || count < 1 {
					t.Fatal("missing-reference worker", count, err)
				}
			} else if err := expireStoppedServicePaymentCheckout(ctx, app, attempt.ID, client); err == nil {
				t.Fatal("unverified result")
			}
			var bound string
			if err := app.DB.QueryRow(ctx, `SELECT coalesce(provider_reference,'') FROM service_payment_attempts WHERE id=$1`, attempt.ID).Scan(&bound); err != nil || bound != reference {
				t.Fatal("reference recovery", bound, err)
			}
			var count int
			if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_checkout_expirations WHERE attempt_id=$1`, attempt.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal("premature closure", count, err)
			}
			if paid {
				if calls != 0 {
					t.Fatal("paid checkout expired")
				}
				if err := RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: "stripe", Reference: reference, EventID: "paid:" + reference, Currency: "USD", TotalCents: 200, PaidAt: time.Now().UTC().Truncate(time.Microsecond)}); err != nil {
					t.Fatal("payment evidence rejected", err)
				}
			} else {
				for i := 0; i < 2; i++ {
					if err := expireStoppedServicePaymentCheckout(ctx, app, attempt.ID, client); err != nil {
						t.Fatal("retry", err)
					}
				}
				if calls != 1 {
					t.Fatal("duplicate expiration", calls)
				}
				if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_checkout_expirations WHERE attempt_id=$1`, attempt.ID).Scan(&count); err != nil || count != 1 {
					t.Fatal("missing proof", count, err)
				}
				var status string
				if err := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, purchase.OrderID).Scan(&status); err != nil || status != "pending" {
					t.Fatal("expiration changed financial state", status, err)
				}
				if _, err := app.DB.Exec(ctx, `DELETE FROM service_payment_checkout_expirations WHERE attempt_id=$1`, attempt.ID); err == nil {
					t.Fatal("expiration proof mutable")
				}
			}
		})
	}
}
