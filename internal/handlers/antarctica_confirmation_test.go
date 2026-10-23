package handlers

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/expedition"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAntarcticaConfirmationDeliveryPostgres(t *testing.T) {
	uri := os.Getenv("EXPEDITION_COMMERCE_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("requires isolated migrated database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{Host: "localhost", MailEndpoint: "http://never-contacted.invalid"}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	for _, scenario := range []string{"retry", "expired", "refund"} {
		t.Run(scenario, func(t *testing.T) {
			var buyer string
			if err := pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Confirmation fixture') RETURNING id::text`).Scan(&buyer); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, buyer, buyer+"@example.test"); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO expedition_inventory(berth,capacity,sales_open,booking_closes_at,hold_seconds) VALUES('shared',18,true,'2028-01-01',900) ON CONFLICT(berth) DO UPDATE SET capacity=18,sales_open=true`); err != nil {
				t.Fatal(err)
			}
			r, err := (&expedition.Store{DB: pool}).ReserveParty(ctx, buyer, uuid.NewString(), expedition.Full, expedition.Stripe, 1, []expedition.CabinSelection{{Berth: "shared", Quantity: 1, Preference: "coed"}})
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Exec(ctx, `DELETE FROM expedition_reservations WHERE id=$1`, r.ID)
			defer pool.Exec(ctx, `DELETE FROM expedition_installments WHERE reservation_id=$1`, r.ID)
			defer pool.Exec(ctx, `DELETE FROM expedition_allocations WHERE reservation_id=$1`, r.ID)
			version := uuid.NewString()
			policy := getters.ServiceLegalPolicy{ProductCode: expedition.ProductCode, Version: version, TermsText: "Fixture", PrivacyText: "Fixture", EffectiveAt: time.Now().UTC().Truncate(time.Microsecond)}
			if err := getters.RegisterServiceLegalPolicy(app, policy); err != nil {
				t.Fatal(err)
			}
			if err := getters.ActivateServiceLegalPolicy(app, policy.ProductCode, version); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
			tax := getters.TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_fixture_" + uuid.NewString(), DestinationCountry: "AR", RawResponse: `{"fixture":true}`, ExpiresAt: &deadline}
			p, err := getters.CreateExpeditionPurchase(app, buyer, r.ID, 1, tax, version)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			key := ""
			send := func(m *emails.Mail) error {
				count++
				if m.Email != buyer+"@example.test" || !strings.Contains(string(m.TextBody), "You’re going to Antarctica") {
					t.Fatal("wrong welcome or recipient")
				}
				if key != "" && m.JobKey != key {
					t.Fatal("retry changed job key")
				}
				key = m.JobKey
				if count == 1 {
					return errors.New("simulated ambiguous mailer failure")
				}
				return nil
			}
			now := time.Now()
			if worked, err := processServiceReceiptWithSender(ctx, app, p.OrderID, now, send); err != nil || worked {
				t.Fatal("unpaid confirmation", worked, err)
			}
			a, err := getters.BeginServicePayment(app, buyer, p.OrderID)
			if err != nil {
				t.Fatal(err)
			}
			ref := "fixture_" + a.ID
			if err := getters.BindServicePayment(app, a.ID, expedition.Stripe, ref); err != nil {
				t.Fatal(err)
			}
			payment := getters.VerifiedServicePayment{AttemptID: a.ID, Provider: expedition.Stripe, Reference: ref, EventID: "fixture_" + a.ID, Currency: "USD", TotalCents: r.Schedule.TotalCents, PaidAt: time.Now()}
			if err := getters.RecordServicePayment(app, payment); err != nil {
				t.Fatal(err)
			}
			now = time.Now().Add(time.Second)
			app.Env.MailOff = true
			if worked, err := processServiceReceiptWithSender(ctx, app, p.OrderID, now, send); err != nil || worked {
				t.Fatal("mail-off consumed job", worked, err)
			}
			app.Env.MailOff = false
			if worked, err := processServiceReceiptWithSender(ctx, app, p.OrderID, now, send); !worked || err == nil || count != 0 {
				t.Fatal("sent before fulfillment", worked, err, count)
			}
			if scenario == "expired" {
				if _, err := pool.Exec(ctx, `UPDATE expedition_reservations SET created_at=now()-interval '2 hours',hold_expires_at=now()-interval '1 hour' WHERE id=$1`, r.ID); err != nil {
					t.Fatal(err)
				}
			}
			err = getters.FulfillExpeditionPurchase(app, p.OrderID)
			if scenario == "expired" {
				if !errors.Is(err, expedition.ErrReview) {
					t.Fatal("expected review", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if scenario == "refund" {
				if _, err := pool.Exec(ctx, `UPDATE shop_orders SET status='refunded' WHERE id=$1`, p.OrderID); err != nil {
					t.Fatal(err)
				}
			}
			worked, err := processServiceReceiptWithSender(ctx, app, p.OrderID, now.Add(6*time.Minute), send)
			if !worked {
				t.Fatal("missing receipt job", err)
			}
			if scenario != "retry" {
				if count != 0 {
					t.Fatal("unconfirmed/refunded welcome sent")
				}
				return
			}
			if err == nil || count != 1 {
				t.Fatal("failed send not retryable", err, count)
			}
			if err := getters.RecordServicePayment(app, payment); err != nil {
				t.Fatal(err)
			}
			if err := getters.FulfillExpeditionPurchase(app, p.OrderID); err != nil {
				t.Fatal(err)
			}
			if worked, err := processServiceReceiptWithSender(ctx, app, p.OrderID, now.Add(17*time.Minute), send); err != nil || !worked || count != 2 {
				t.Fatal("retry failed", worked, err, count)
			}
			if worked, err := processServiceReceiptWithSender(ctx, app, p.OrderID, now.Add(time.Hour), send); err != nil || worked || count != 2 {
				t.Fatal("accepted welcome replayed", worked, err, count)
			}
		})
	}
}
