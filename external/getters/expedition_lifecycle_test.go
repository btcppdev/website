package getters

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func lifecycleFixture(t *testing.T) (*config.AppContext, expedition.Reservation, ExpeditionLifecycleCandidate) {
	t.Helper()
	uri := os.Getenv("EXPEDITION_COMMERCE_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("isolated migrated database required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	var buyer string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name)VALUES('Lifecycle fixture')RETURNING id::text`).Scan(&buyer); err != nil {
		t.Fatal(err)
	}
	email := buyer + "@example.test"
	if _, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at)VALUES($1,$2,true,now())`, buyer, email); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE expedition_inventory SET capacity=18,sales_open=true,booking_closes_at='2028-01-01',hold_seconds=3600 WHERE berth='shared'`); err != nil {
		t.Fatal(err)
	}
	r, err := (&expedition.Store{DB: pool}).Reserve(ctx, buyer, uuid.NewString(), "shared", expedition.Installments, expedition.Bitcoin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM expedition_lifecycle_mail WHERE event_id IN(SELECT id FROM expedition_lifecycle_events WHERE reservation_id=$1)`, r.ID)
		pool.Exec(ctx, `DELETE FROM expedition_lifecycle_events WHERE reservation_id=$1`, r.ID)
		pool.Exec(ctx, `DELETE FROM expedition_allocations WHERE reservation_id=$1`, r.ID)
		pool.Exec(ctx, `DELETE FROM expedition_installments WHERE reservation_id=$1`, r.ID)
		pool.Exec(ctx, `DELETE FROM expedition_reservations WHERE id=$1`, r.ID)
	})
	policy := ServiceLegalPolicy{ProductCode: expedition.ProductCode, Version: uuid.NewString(), TermsText: "Fixture: 30-day abandonment retains payments; America/Chicago", PrivacyText: "Fixture only", EffectiveAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err = RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal("register", policy, err)
	}
	if err = ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal("activate", err)
	}
	expiry := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
	p, err := CreateExpeditionPurchase(app, buyer, r.ID, 1, TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_" + uuid.NewString(), DestinationCountry: "US", RawResponse: `{"fixture":true}`, ExpiresAt: &expiry}, policy.Version)
	if err != nil {
		t.Fatal(err)
	}
	settleLifecycleOrder(t, app, p, 334000)
	// Keep the test's later installment in the current payable window.
	due := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 5)
	r.Schedule.Installments[1].DueAt = due
	raw, _ := json.Marshal(r.Schedule)
	if _, err = pool.Exec(ctx, `UPDATE expedition_reservations SET schedule=$2 WHERE id=$1`, r.ID, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE expedition_installments SET due_at=$2 WHERE reservation_id=$1 AND number=2`, r.ID, due); err != nil {
		t.Fatal(err)
	}
	c := ExpeditionLifecycleCandidate{ID: r.ID, Buyer: buyer, Recipient: email, Name: "Lifecycle fixture", Policy: policy.Version, Zone: "America/Chicago", Number: 2, Due: due, Cents: 334000}
	return app, r, c
}
func settleLifecycleOrder(t *testing.T, app *config.AppContext, p *ServicePurchase, cents int64) {
	t.Helper()
	var err error
	// ServicePurchase has no person field; resolve purchaser via immutable request.
	var buyer string
	if err = app.DB.QueryRow(context.Background(), `SELECT person_id::text FROM service_purchase_requests WHERE order_id=$1`, p.OrderID).Scan(&buyer); err != nil {
		t.Fatal(err)
	}
	a, err := BeginServicePayment(app, buyer, p.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	ref := "lifecycle-" + a.ID
	if err = BindServicePayment(app, a.ID, expedition.Bitcoin, ref); err != nil {
		t.Fatal(err)
	}
	payment := VerifiedServicePayment{AttemptID: a.ID, Provider: expedition.Bitcoin, Reference: ref, EventID: "paid-" + a.ID, Currency: "USD", TotalCents: cents, PaidAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err = RecordServicePayment(app, payment); err != nil {
		t.Fatal(err)
	}
	if err = FulfillExpeditionPurchase(app, p.OrderID); err != nil {
		t.Fatal(err)
	}
}
func approveLifecycle(t *testing.T, app *config.AppContext, c ExpeditionLifecycleCandidate) {
	t.Helper()
	if _, err := app.DB.Exec(context.Background(), `INSERT INTO expedition_lifecycle_policies(version,time_zone)VALUES($1,$2)`, c.Policy, c.Zone); err != nil {
		t.Fatal(err)
	}
}

func TestExpeditionLifecyclePostgres(t *testing.T) {
	app, r, c := lifecycleFixture(t)
	ctx := context.Background()
	list, err := ExpeditionLifecycleCandidates(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range list {
		if v.ID == r.ID {
			t.Fatal("unapproved old terms enrolled")
		}
	}
	approveLifecycle(t, app, c)
	now, _ := expedition.InstallmentReminderAt(c.Due, c.Zone, -4)
	if ok, err := ProcessExpeditionLifecycle(ctx, app, c, now); err != nil || !ok {
		t.Fatal("reminder", ok, err)
	}
	if ok, err := ProcessExpeditionLifecycle(ctx, app, c, now); err != nil || ok {
		t.Fatal("duplicate reminder", ok, err)
	}
	j, err := ClaimExpeditionLifecycleMail(ctx, app, now)
	if err != nil {
		t.Fatal(err)
	}
	if j.Kind != "reminder" || j.Snapshot.Offset != -4 {
		t.Fatal("wrong reminder", j)
	}
	if err = FinishExpeditionLifecycleMail(ctx, app, j, false, now); err != nil {
		t.Fatal(err)
	}
	retry, err := ClaimExpeditionLifecycleMail(ctx, app, now.Add(11*time.Minute))
	if err != nil || retry.EventID != j.EventID || retry.Attempt != 2 {
		t.Fatal("retry identity", retry, err)
	}
	if err = FinishExpeditionLifecycleMail(ctx, app, j, true, now); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = app.DB.QueryRow(ctx, `SELECT state FROM expedition_lifecycle_mail WHERE event_id=$1 AND audience='customer'`, j.EventID).Scan(&state); err != nil || state != "pending" {
		t.Fatal("stale attempt accepted", state, err)
	}
	if err = FinishExpeditionLifecycleMail(ctx, app, retry, true, now); err != nil {
		t.Fatal(err)
	}
	deadline, _ := expedition.InstallmentDeadline(c.Due, c.Zone)
	cutoff := deadline.AddDate(0, 0, 30)
	before, err := (&expedition.Store{DB: app.DB}).Availability(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := ProcessExpeditionLifecycle(ctx, app, c, cutoff); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	after, err := (&expedition.Store{DB: app.DB}).Availability(ctx)
	if err != nil || after.Remaining != before.Remaining+1 {
		t.Fatal("berth release", before, after, err)
	}
	var confirmed bool
	if err = app.DB.QueryRow(ctx, `SELECT state,confirmed_at IS NOT NULL FROM expedition_reservations WHERE id=$1`, r.ID).Scan(&state, &confirmed); err != nil || state != "abandoned" || !confirmed {
		t.Fatal("lost historical confirmation", state, confirmed, err)
	}
	var events, jobs int
	if err = app.DB.QueryRow(ctx, `SELECT count(*),(SELECT count(*)FROM expedition_lifecycle_mail m JOIN expedition_lifecycle_events e ON e.id=m.event_id WHERE e.reservation_id=$1 AND e.kind='abandoned')FROM expedition_lifecycle_events WHERE reservation_id=$1 AND kind='abandoned'`, r.ID).Scan(&events, &jobs); err != nil || events != 1 || jobs != 2 {
		t.Fatal("atomic notification count", events, jobs, err)
	}
	for n := 0; n < 2; n++ {
		j, err = ClaimExpeditionLifecycleMail(ctx, app, cutoff)
		if err != nil || j.Kind != "abandoned" || j.Snapshot.RetainedCents != 334000 {
			t.Fatal("forfeiture notice", j, err)
		}
		if j.Audience == "staff" && j.Recipient != "inbox@btcpp.dev" {
			t.Fatal("wrong staff address")
		}
		FinishExpeditionLifecycleMail(ctx, app, j, true, cutoff)
	}
	if _, err = (&expedition.Store{DB: app.DB}).Charge(ctx, c.Buyer, r.ID, 2); err == nil {
		t.Fatal("abandoned booking payable")
	}
}

func TestExpeditionInstallmentReplacementAndLatePaymentPostgres(t *testing.T) {
	app, r, c := lifecycleFixture(t)
	ctx := context.Background()
	approveLifecycle(t, app, c)
	expiry := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Microsecond)
	tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_" + uuid.NewString(), DestinationCountry: "US", RawResponse: `{"fixture":true}`, ExpiresAt: &expiry}
	key := "expedition:" + r.ID + ":2:" + uuid.NewString()
	p, err := CreateExpeditionInstallmentPurchase(app, c.Buyer, r.ID, 2, key, tax, c.Policy)
	if err != nil {
		t.Fatal(err)
	}
	// A second browser key must not create a parallel payable installment.
	if _, err = CreateExpeditionInstallmentPurchase(app, c.Buyer, r.ID, 2, "expedition:"+r.ID+":2:"+uuid.NewString(), tax, c.Policy); err == nil {
		t.Fatal("parallel order allowed")
	}
	a, err := BeginServicePayment(app, c.Buyer, p.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if err = BindServicePayment(app, a.ID, expedition.Bitcoin, "late-"+a.ID); err != nil {
		t.Fatal(err)
	}
	deadline, _ := expedition.InstallmentDeadline(c.Due, c.Zone)
	cutoff := deadline.AddDate(0, 0, 30)
	if ok, err := ProcessExpeditionLifecycle(ctx, app, c, cutoff); err != nil || ok {
		t.Fatal("unresolved payment forfeited", ok, err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO expedition_payment_checks(attempt_id,checked_at,status,evidence)VALUES($1,$2,'expired','{}')`, a.ID, cutoff); err != nil {
		t.Fatal(err)
	}
	if ok, err := ProcessExpeditionLifecycle(ctx, app, c, cutoff); err != nil || !ok {
		t.Fatal("terminal expired payment blocked", ok, err)
	}
	payment := VerifiedServicePayment{AttemptID: a.ID, Provider: expedition.Bitcoin, Reference: "late-" + a.ID, EventID: "late-event-" + a.ID, Currency: "USD", TotalCents: 334000, PaidAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err = RecordServicePayment(app, payment); err != nil {
		t.Fatal(err)
	}
	// Shared commerce may leave the cancelled order for manual reconciliation.
	if err = FulfillExpeditionPurchase(app, p.OrderID); err == nil {
		t.Fatal("late payment restored booking")
	}
	var state string
	var released bool
	if err = app.DB.QueryRow(ctx, `SELECT state,released_at IS NOT NULL FROM expedition_reservations WHERE id=$1`, r.ID).Scan(&state, &released); err != nil || state != "abandoned" || !released {
		t.Fatal(state, released, err)
	}
}

func TestExpeditionPaidSuppressesReminderPostgres(t *testing.T) {
	app, r, c := lifecycleFixture(t)
	ctx := context.Background()
	approveLifecycle(t, app, c)
	now, _ := expedition.InstallmentReminderAt(c.Due, c.Zone, -4)
	if _, err := ProcessExpeditionLifecycle(ctx, app, c, now); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Microsecond)
	tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_" + uuid.NewString(), DestinationCountry: "US", RawResponse: `{"fixture":true}`, ExpiresAt: &expiry}
	p, err := CreateExpeditionInstallmentPurchase(app, c.Buyer, r.ID, 2, "expedition:"+r.ID+":2:"+uuid.NewString(), tax, c.Policy)
	if err != nil {
		t.Fatal(err)
	}
	settleLifecycleOrder(t, app, p, 334000)
	if _, err = ClaimExpeditionLifecycleMail(ctx, app, now); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("paid customer reminded", err)
	}
	deadline, _ := expedition.InstallmentDeadline(c.Due, c.Zone)
	if ok, err := ProcessExpeditionLifecycle(ctx, app, c, deadline.AddDate(0, 0, 30)); err != nil || ok {
		t.Fatal("stale unpaid candidate forfeited", ok, err)
	}
	b, err := ExpeditionBookingForPerson(ctx, app, c.Buyer, r.ID)
	if err != nil || len(b.Payments) != 5 || !b.Payments[1].Applied {
		t.Fatal("booking payment projection", b, err)
	}
}

func TestExpeditionExpiredQuoteReplacementPostgres(t *testing.T) {
	app, r, c := lifecycleFixture(t)
	ctx := context.Background()
	approveLifecycle(t, app, c)
	expiry := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_" + uuid.NewString(), DestinationCountry: "US", RawResponse: `{"fixture":true}`, ExpiresAt: &expiry}
	p, err := CreateExpeditionInstallmentPurchase(app, c.Buyer, r.ID, 2, "expedition:"+r.ID+":2:"+uuid.NewString(), tax, c.Policy)
	if err != nil {
		t.Fatal(err)
	}
	if existing, err := ExpeditionInstallmentOrder(ctx, app, c.Buyer, r.ID, 2); err != nil || existing != p.PublicID {
		t.Fatal("live order lost", existing, err)
	}
	time.Sleep(time.Until(expiry.Add(10 * time.Millisecond)))
	if existing, err := ExpeditionInstallmentOrder(ctx, app, c.Buyer, r.ID, 2); err != nil || existing != "" {
		t.Fatal("expired no-attempt order not retired", existing, err)
	}
	expiry = time.Now().UTC().Add(5 * time.Minute).Truncate(time.Microsecond)
	tax.ProviderQuoteID = "taxcalc_" + uuid.NewString()
	replacement, err := CreateExpeditionInstallmentPurchase(app, c.Buyer, r.ID, 2, "expedition:"+r.ID+":2:"+uuid.NewString(), tax, c.Policy)
	if err != nil || replacement.OrderID == p.OrderID {
		t.Fatal("replacement", replacement, err)
	}
	settleLifecycleOrder(t, app, replacement, 334000)
	b, err := ExpeditionBookingForPerson(ctx, app, c.Buyer, r.ID)
	if err != nil || len(b.Payments) != 5 || b.Payments[1].OrderPublicID != replacement.PublicID || !b.Payments[1].Applied {
		t.Fatal("replacement projection", b, err)
	}
}

func TestExpeditionPaymentAtForfeitureCutoffPostgres(t *testing.T) {
	app, r, c := lifecycleFixture(t)
	approveLifecycle(t, app, c)
	ctx := context.Background()
	expiry := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Microsecond)
	tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_" + uuid.NewString(), DestinationCountry: "US", RawResponse: `{"fixture":true}`, ExpiresAt: &expiry}
	p, err := CreateExpeditionInstallmentPurchase(app, c.Buyer, r.ID, 2, "expedition:"+r.ID+":2:"+uuid.NewString(), tax, c.Policy)
	if err != nil {
		t.Fatal(err)
	}
	a, err := BeginServicePayment(app, c.Buyer, p.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	ref := "cutoff-" + a.ID
	if err = BindServicePayment(app, a.ID, expedition.Bitcoin, ref); err != nil {
		t.Fatal(err)
	}
	if err = RecordServicePayment(app, VerifiedServicePayment{AttemptID: a.ID, Provider: expedition.Bitcoin, Reference: ref, EventID: "paid-" + a.ID, Currency: "USD", TotalCents: 334000, PaidAt: time.Now().UTC().Truncate(time.Microsecond)}); err != nil {
		t.Fatal(err)
	}
	// Simulate recorded settlement at the cutoff without waiting thirty days.
	deadline, _ := expedition.InstallmentDeadline(c.Due, c.Zone)
	if _, err = app.DB.Exec(ctx, `UPDATE shop_orders SET paid_at=$2 WHERE id=$1`, p.OrderID, deadline.AddDate(0, 0, 30)); err != nil {
		t.Fatal(err)
	}
	if err = FulfillExpeditionPurchase(app, p.OrderID); !errors.Is(err, expedition.ErrReview) {
		t.Fatal("late settlement must require review", err)
	}
	var state string
	var released *time.Time
	if err = app.DB.QueryRow(ctx, `SELECT state,released_at FROM expedition_reservations WHERE id=$1`, r.ID).Scan(&state, &released); err != nil || state != "review" || released != nil {
		t.Fatal("ambiguous payment lost berths", state, released, err)
	}
	if err = FulfillExpeditionPurchase(app, p.OrderID); !errors.Is(err, expedition.ErrReview) {
		t.Fatal("retry cleared review", err)
	}
}
