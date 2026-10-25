package getters

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
	"github.com/stripe/stripe-go/v86"
)

func TestBitcoinSwitchPendingCardPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	for _, scenario := range []string{"open", "timeout", "paid", "expired_quote"} {
		t.Run(scenario, func(t *testing.T) {
			var person string
			if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Bitcoin switch fixture') RETURNING id::text`).Scan(&person); err != nil {
				t.Fatal(err)
			}
			defer app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
			if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
				t.Fatal(err)
			}
			card, err := ReserveAlertsStripeCheckout(app, person, "switch-card-"+person, 1, "https://btcpp.dev")
			if err != nil {
				t.Fatal(err)
			}
			ref := "cs_" + card.Intent.ID
			if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$2,provider_started_at=now() WHERE id=$1`, card.Intent.ID, ref); err != nil {
				t.Fatal(err)
			}
			revision, err := AlertsBillingRevision(app, person)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now().UTC()
			end := start.AddDate(1, 0, 0)
			expiry := start.Add(10 * time.Minute)
			quote := ServiceQuote{AccountRevision: revision, BillingMonths: 12, ProductCode: "alerts.repository-slots", Name: "Alerts", Provider: "opennode", Quantity: 1, UnitCents: 2400, TotalCents: 2400, TaxCents: 200, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: expiry}
			tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_switch_" + person, SalesTaxAmountCents: 200, DestinationCountry: "US", RawResponse: `{"amount_total":2400}`, ExpiresAt: &expiry}
			var saved *ServiceCheckoutQuote
			if scenario == "expired_quote" {
				quote.ExpiresAt = start.Add(-time.Minute)
				rawQuote, e := json.Marshal(quote)
				if e != nil {
					t.Fatal(e)
				}
				rawTax, e := json.Marshal(tax)
				if e != nil {
					t.Fatal(e)
				}
				saved = &ServiceCheckoutQuote{}
				// Insert an immutable historical fixture; do not rewrite a reviewed quote.
				if err = app.DB.QueryRow(ctx, `INSERT INTO service_checkout_quotes(person_id,quote_snapshot,tax_snapshot,expires_at) VALUES($1,$2,$3,$4) RETURNING id::text`, person, rawQuote, rawTax, quote.ExpiresAt).Scan(&saved.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				saved, err = SaveAlertsCheckoutQuote(app, person, quote, tax)
				if err != nil {
					t.Fatal(err)
				}
			}
			session := &stripe.CheckoutSession{ID: ref, Mode: "subscription", Status: "open", PaymentStatus: "unpaid", ClientReferenceID: card.Intent.ID, Metadata: map[string]string{"account_id": person, "checkout_intent": card.Intent.ID, "product": "alerts.repository-slots"}}
			if scenario == "paid" {
				session.Status = "complete"
				session.PaymentStatus = "paid"
			}
			expires := 0
			client := alertsstripe.Client{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }, Expire: func(string, *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
				expires++
				if scenario == "timeout" {
					return nil, errors.New("fixture timeout")
				}
				session.Status = "expired"
				return session, nil
			}}
			purchase, err := ConfirmServiceCheckoutQuoteSwitchingToBitcoin(ctx, app, person, saved.ID, "", client)
			var orders int
			if e := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_purchase_requests WHERE person_id=$1`, person).Scan(&orders); e != nil {
				t.Fatal(e)
			}
			cardAfter, e := AlertsStripeCheckoutForPerson(app, person, card.Intent.ID)
			if e != nil {
				t.Fatal(e)
			}
			if scenario == "open" {
				if err != nil || purchase == nil || orders != 1 || !cardAfter.Closed || expires != 1 {
					t.Fatal(purchase, err, orders, cardAfter, expires)
				}
				again, e := ConfirmServiceCheckoutQuoteSwitchingToBitcoin(ctx, app, person, saved.ID, "", client)
				if e != nil || again.OrderID != purchase.OrderID || expires != 1 {
					t.Fatal("retry changed order or cancelled twice", again, e, expires)
				}
			} else {
				if err == nil || purchase != nil || orders != 0 || cardAfter.Closed {
					t.Fatal("unsafe payment switch", purchase, err, orders, cardAfter)
				}
				if (scenario == "paid" || scenario == "expired_quote") && expires != 0 {
					t.Fatal("checkout cancelled without valid switch", expires)
				}
			}
		})
	}
}
