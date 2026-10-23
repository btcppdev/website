package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"os"
	"testing"
	"time"
)

func TestExpeditionSharedCommerce(t *testing.T) {
	for _, provider := range []string{expedition.Stripe, expedition.Bitcoin} {
		t.Run(provider, func(t *testing.T) { testExpeditionSharedCommerce(t, provider) })
	}
}

func testExpeditionSharedCommerce(t *testing.T, provider string) {
	total := int64(4928700)
	if provider == expedition.Bitcoin {
		total = 4682265
	}
	url := os.Getenv("EXPEDITION_COMMERCE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("explicit disposable migrated database required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	var buyer string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Expedition integration fixture') RETURNING id::text`).Scan(&buyer); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, buyer, buyer+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO expedition_inventory(berth,capacity,sales_open,booking_closes_at,hold_seconds) VALUES('shared',18,true,'2028-01-01',900) ON CONFLICT(berth) DO UPDATE SET capacity=18,sales_open=true`); err != nil {
		t.Fatal(err)
	}
	store := &expedition.Store{DB: pool}
	r, err := store.ReserveParty(ctx, buyer, uuid.NewString(), expedition.Full, provider, 3, []expedition.CabinSelection{{Berth: "shared", Quantity: 2, Preference: "coed"}, {Berth: "shared", Quantity: 1, Preference: "female"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM expedition_allocations WHERE reservation_id=$1`, r.ID)
		pool.Exec(ctx, `DELETE FROM expedition_installments WHERE reservation_id=$1`, r.ID)
		pool.Exec(ctx, `DELETE FROM expedition_reservations WHERE id=$1`, r.ID)
	})
	deadline := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
	tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_fixture_" + uuid.NewString(), DestinationCountry: "AR", RawResponse: `{"fixture":true}`, ExpiresAt: &deadline}
	if _, err = CreateExpeditionPurchase(app, buyer, r.ID, 1, tax, ""); !errors.Is(err, ErrServiceLegalReview) {
		t.Fatal("missing terms allowed", err)
	}
	version := uuid.NewString()
	p := ServiceLegalPolicy{ProductCode: expedition.ProductCode, Version: version, TermsText: "Test-only terms", PrivacyText: "Test-only privacy", EffectiveAt: time.Now().UTC().Add(-time.Minute).Truncate(time.Second)}
	if err = RegisterServiceLegalPolicy(app, p); err != nil {
		t.Fatal(err)
	}
	if err = ActivateServiceLegalPolicy(app, p.ProductCode, p.Version); err != nil {
		t.Fatal(err)
	}
	purchase, err := CreateExpeditionPurchase(app, buyer, r.ID, 1, tax, version)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := CreateExpeditionPurchase(app, buyer, r.ID, 1, tax, version)
	if err != nil || retry.OrderID != purchase.OrderID {
		t.Fatal("order retry", retry, err)
	}
	if err = FulfillExpeditionPurchase(app, purchase.OrderID); err == nil {
		t.Fatal("unpaid order fulfilled")
	}
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT quote_snapshot FROM service_purchase_requests WHERE order_id=$1`, purchase.OrderID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var q ServiceQuote
	if err = json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	if q.TotalCents != total || q.LegalAcceptance == nil || q.LegalAcceptance.Version != version || q.RecurringInvoice {
		t.Fatal("wrong financial snapshot", q)
	}
	q.LegalAcceptance = nil
	q.TotalCents--
	q.UnitCents--
	if _, err = CreateTaxedServicePurchaseWithPolicy(app, buyer, uuid.NewString(), q, tax, version); err == nil {
		t.Fatal("forged price accepted")
	}
	attempt, err := BeginServicePayment(app, buyer, purchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	ref := "fixture_" + attempt.ID
	if err = BindServicePayment(app, attempt.ID, provider, ref); err != nil {
		t.Fatal(err)
	}
	payment := VerifiedServicePayment{AttemptID: attempt.ID, Provider: provider, Reference: ref, EventID: "fixture-event-" + attempt.ID, Currency: "USD", TotalCents: total, PaidAt: time.Now().UTC().Truncate(time.Microsecond)}
	for n := 0; n < 2; n++ {
		if err = RecordServicePayment(app, payment); err != nil {
			t.Fatal(err)
		}
	}
	for n := 0; n < 2; n++ {
		if err = FulfillExpeditionPurchase(app, purchase.OrderID); err != nil {
			t.Fatal(err)
		}
	}
	var state string
	var count int
	if err = pool.QueryRow(ctx, `SELECT state FROM expedition_reservations WHERE id=$1`, r.ID).Scan(&state); err != nil || state != "confirmed" {
		t.Fatal(state, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM service_fulfillment_jobs WHERE attempt_id=$1`, attempt.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("dispatch count", count, err)
	}
	if _, err = CreateExpeditionPurchase(app, buyer, r.ID, 1, tax, version); err != nil {
		t.Fatal("paid order retry", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE shop_orders SET status='refunded' WHERE id=$1`, purchase.OrderID); err != nil {
		t.Fatal(err)
	}
	if err = FulfillExpeditionPurchase(app, purchase.OrderID); err == nil {
		t.Fatal("refunded order fulfilled")
	}
}
