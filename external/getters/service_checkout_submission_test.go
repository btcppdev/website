package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/servicestripe"
	"github.com/google/uuid"
)

func TestStoppedUnsubmittedCheckoutPostgres(t *testing.T) {
	for _, service := range []string{"alerts", "insider"} {
		for _, mode := range []string{"reserved", "submitted", "legacy", "bound-reference", "race"} {
			t.Run(service+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				app := databaseSmokeContext(t)
				var person string
				if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Unsubmitted closure fixture') RETURNING id::text`).Scan(&person); err != nil {
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
				if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
					t.Fatal(err)
				}
				if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
					t.Fatal(err)
				}
				product, id := "alerts.repository-slots", ""
				if service == "alerts" {
					checkout, err := ReserveAlertsStripeCheckout(app, person, uuid.NewString(), 1, "https://btcpp.dev")
					if err != nil {
						t.Fatal(err)
					}
					id = checkout.Intent.ID
				} else {
					product = "insider.membership.monthly"
					checkout, err := ReserveServiceStripeCheckout(app, person, uuid.NewString(), servicestripe.CheckoutIntent{ProductCode: product, Quantity: 1, TaxCode: "txcd_test", ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/success", CancelURL: "https://insider.btcpp.dev/cancel"})
					if err != nil {
						t.Fatal(err)
					}
					id = checkout.Intent.ID
				}
				if closed, err := closeUnsubmittedStoppedServiceCheckout(ctx, app, person, id, product); err != nil || closed {
					t.Fatal("active reservation closed", closed, err)
				}
				if err := markServiceStripeCheckoutStarted(ctx, app, uuid.NewString(), id, product); !errors.Is(err, ErrServiceStripeCheckoutConflict) {
					t.Fatal("foreign claim", err)
				}
				switch mode {
				case "submitted":
					if err := markServiceStripeCheckoutStarted(ctx, app, person, id, product); err != nil {
						t.Fatal(err)
					}
					var before, after time.Time
					if err := app.DB.QueryRow(ctx, `SELECT provider_started_at FROM service_stripe_checkouts WHERE id=$1`, id).Scan(&before); err != nil {
						t.Fatal(err)
					}
					if err := markServiceStripeCheckoutStarted(ctx, app, person, id, product); err != nil {
						t.Fatal(err)
					}
					if err := app.DB.QueryRow(ctx, `SELECT provider_started_at FROM service_stripe_checkouts WHERE id=$1`, id).Scan(&after); err != nil || !before.Equal(after) {
						t.Fatal("submission evidence changed", err)
					}
					if _, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET provider_started_at=NULL WHERE id=$1`, id); err == nil {
						t.Fatal("submission evidence erased")
					}
				case "legacy":
					// Old writers/imports omit the new field. Exercise the conservative default.
					legacy := uuid.NewString()
					if _, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET closed_at=now() WHERE id=$1`, id); err != nil {
						t.Fatal(err)
					}
					if _, err := app.DB.Exec(ctx, `INSERT INTO service_stripe_checkouts(id,person_id,request_key,product_code,intent) SELECT $2::text::uuid,person_id,$2::text,product_code,intent FROM service_stripe_checkouts WHERE id=$1`, id, legacy); err != nil {
						t.Fatal(err)
					}
					id = legacy
				case "bound-reference":
					if _, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET stripe_session_id=$2 WHERE id=$1`, id, "cs_"+id); err != nil {
						t.Fatal(err)
					}
				}
				var claimResult chan error
				if mode == "race" {
					claimResult = make(chan error, 1)
					go func() { claimResult <- markServiceStripeCheckoutStarted(ctx, app, person, id, product) }()
				}
				if err := StopServiceSales(ctx, app, person, service, "Closure fixture"); err != nil {
					t.Fatal(err)
				}
				if claimResult != nil {
					switch claimErr := <-claimResult; {
					case claimErr == nil:
						mode = "submitted"
					case errors.Is(claimErr, ErrServiceSalesClosed):
						mode = "reserved"
					default:
						t.Fatal("submission/closure race", claimErr)
					}
				}
				if err := markServiceStripeCheckoutStarted(ctx, app, person, id, product); !errors.Is(err, ErrServiceSalesClosed) {
					t.Fatal("submission after closure", err)
				}
				closed, err := closeUnsubmittedStoppedServiceCheckout(ctx, app, person, id, product)
				if err != nil || closed != (mode == "reserved") {
					t.Fatal("closure proof mismatch", closed, err)
				}
				var savedClosed bool
				if err := app.DB.QueryRow(ctx, `SELECT closed_at IS NOT NULL FROM service_stripe_checkouts WHERE id=$1`, id).Scan(&savedClosed); err != nil || savedClosed != closed {
					t.Fatal("closure not persisted", savedClosed, err)
				}
			})
		}
	}
}
