package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/servicestripe"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestStoppedSalesExpireCheckoutPostgres(t *testing.T) {
	for _, service := range []string{"alerts", "insider"} {
		for _, mode := range []string{"lost-expiration-response", "complete", "wrong-owner", "lost-creation-reference"} {
			t.Run(service+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				app := databaseSmokeContext(t)
				var person string
				if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Closure checkout operator') RETURNING id::text`).Scan(&person); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := app.DB.Exec(ctx, `TRUNCATE service_closure_plans,service_sales_closures`); err != nil {
						t.Error(err)
					}
					if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
						t.Error(err)
					}
				})
				if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
					t.Fatal(err)
				}
				if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
					t.Fatal(err)
				}
				product, table, id := "alerts.repository-slots", "alerts_stripe_checkouts", ""
				if service == "alerts" {
					stored, err := ReserveAlertsStripeCheckout(app, person, uuid.NewString(), 1, "https://btcpp.dev")
					if err != nil {
						t.Fatal(err)
					}
					id = stored.Intent.ID
				} else {
					product, table = "insider.membership.monthly", "service_stripe_checkouts"
					stored, err := ReserveServiceStripeCheckout(app, person, uuid.NewString(), servicestripe.CheckoutIntent{ProductCode: product, Quantity: 1, TaxCode: "txcd_test", ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/success", CancelURL: "https://insider.btcpp.dev/cancel"})
					if err != nil {
						t.Fatal(err)
					}
					id = stored.Intent.ID
				}
				session := &stripe.CheckoutSession{ID: "cs_" + id, Created: time.Now().Unix(), Mode: "subscription", Status: "open", PaymentStatus: "unpaid", ClientReferenceID: id, Metadata: map[string]string{"account_id": person, "checkout_intent": id, "product": product}}
				if mode != "lost-creation-reference" {
					if _, err := app.DB.Exec(ctx, `UPDATE `+table+` SET stripe_session_id=$2 WHERE id=$1`, id, session.ID); err != nil {
						t.Fatal(err)
					}
				}
				expires, reads := 0, 0
				client := servicestripe.CheckoutClient{
					Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
						reads++
						return session, nil
					},
					List: func(*stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
						return &stripe.CheckoutSessionList{Data: []*stripe.CheckoutSession{session}}, nil
					},
					Expire: func(ref string, p *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
						expires++
						if ref != session.ID || p.IdempotencyKey == nil {
							t.Fatal("unbound expiration")
						}
						session.Status = "expired"
						if mode == "lost-expiration-response" {
							return nil, errors.New("lost response")
						}
						return session, nil
					},
				}
				run := func() (bool, error) {
					if service == "alerts" {
						adapter := alertsstripe.Client{Retrieve: client.Retrieve, Expire: client.Expire, ListCheckouts: client.List}
						if _, err := recoverAlertsStripeCheckoutReference(ctx, app, person, id, adapter); err != nil {
							return true, err
						}
						return expireConflictingAlertsCheckout(ctx, app, person, id, adapter)
					}
					return expireStoppedServiceStripeCheckout(ctx, app, person, id, product, client)
				}
				if handled, err := run(); err != nil || handled || expires != 0 {
					t.Fatal("active service expired checkout", handled, err)
				}
				if err := StopServiceSales(ctx, app, person, service, "Permanent closure"); err != nil {
					t.Fatal(err)
				}
				if mode == "complete" {
					session.Status = "complete"
					session.PaymentStatus = "paid"
				}
				if mode == "wrong-owner" {
					session.Metadata["account_id"] = uuid.NewString()
				}
				handled, err := run()
				var closed bool
				if e := app.DB.QueryRow(ctx, `SELECT closed_at IS NOT NULL FROM `+table+` WHERE id=$1`, id).Scan(&closed); e != nil {
					t.Fatal(e)
				}
				switch mode {
				case "complete":
					if handled || err != nil || closed || expires != 0 {
						t.Fatal("completed payment not delegated", handled, err, closed, expires)
					}
				case "wrong-owner":
					if err == nil || closed || expires != 0 {
						t.Fatal("foreign checkout expired", err, closed, expires)
					}
				case "lost-expiration-response":
					if err == nil || closed || expires != 1 {
						t.Fatal("unverified closure", err, closed, expires)
					}
					handled, err = run()
					if !handled || err != nil || expires != 1 {
						t.Fatal("expiration retry", handled, err, expires)
					}
					if e := app.DB.QueryRow(ctx, `SELECT closed_at IS NOT NULL FROM `+table+` WHERE id=$1`, id).Scan(&closed); e != nil || !closed {
						t.Fatal("retry did not close", e)
					}
				default:
					if !handled || err != nil || !closed || expires != 1 {
						t.Fatal("lost-reference expiration", handled, err, closed, expires)
					}
				}
				if reads == 0 {
					t.Fatal("provider evidence not retrieved")
				}
			})
		}
	}
}
