package getters

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestAlertsStripeIncreaseReservationPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Stripe increase reservation') RETURNING id::text`).Scan(&person); err != nil {
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
	checkout, err := ReserveAlertsStripeCheckout(app, person, "increase-reservation", 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	id := checkout.Intent.ID
	now := time.Now().UTC().Truncate(time.Second)
	preview := alertsstripe.IncreasePreview{SubscriptionItemID: "si_test", PreviousSlots: 2, PaidSlots: 3, PeriodStart: now.Add(-time.Hour).Unix(), PeriodEnd: now.AddDate(0, 1, 0).Unix(), ProrationDate: now.Unix(), TotalCents: 195, TaxCents: 15, ExpiresAt: now.Add(10 * time.Minute)}
	b, _ := json.Marshal(alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, PaidSlots: 2})
	s, _ := json.Marshal(alertsstripe.SubscriptionState{State: "active", ProviderStatus: "active", PeriodStart: preview.PeriodStart, PeriodEnd: preview.PeriodEnd})
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, id, "sub_"+id, b); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscription_state(checkout_id,revision,snapshot,observed_at) VALUES($1,1,$2,now())`, id, s); err != nil {
		t.Fatal(err)
	}
	quote, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, preview)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, preview)
	if err != nil {
		t.Fatal(err)
	}
	expired := preview
	expired.ExpiresAt = now.Add(-time.Second)
	if _, err = SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, expired); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("expired preview saved", err)
	}
	// Seed a quote that was valid when issued, but expired before confirmation.
	expired.ProrationDate = now.Add(-11 * time.Minute).Unix()
	expiredRaw, _ := json.Marshal(expired)
	var expiredID string
	if err = app.DB.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_quotes(checkout_id,revision,preview,created_at,expires_at) VALUES($1,1,$2,$3,$4) RETURNING id::text`, id, expiredRaw, now.Add(-11*time.Minute), expired.ExpiresAt).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, expiredID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("expired quote confirmed", err)
	}
	if _, err = AlertsStripeIncreaseQuoteForPerson(ctx, app, uuid.NewString(), quote.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign quote readable", err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, uuid.NewString(), quote.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign quote confirmable", err)
	}
	if _, err = SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 2, preview); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale revision", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_quotes SET preview='{}' WHERE id=$1`, quote.ID); err == nil {
		t.Fatal("quote mutable")
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, e := ReserveAlertsStripeIncrease(ctx, app, person, quote.ID)
			if e != nil || status != "pending" {
				t.Errorf("reservation %s %v", status, e)
			}
		}()
	}
	wg.Wait()
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("overlapping upgrade", err)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, id, uuid.NewString(), 1, preview.PeriodEnd); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("cancellation during upgrade", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increases SET state='expired' WHERE quote_id=$1`, quote.ID); err != nil {
		t.Fatal(err)
	}
	if status, e := ReserveAlertsStripeIncrease(ctx, app, person, quote.ID); e != nil || status != "expired" {
		t.Fatal("resolved retry", status, e)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increases SET state='pending' WHERE quote_id=$1`, quote.ID); err == nil {
		t.Fatal("resolved instruction reopened")
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=2 WHERE checkout_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale confirmation", err)
	}
	fresh, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 2, preview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, id, uuid.NewString(), 2, preview.PeriodEnd); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, fresh.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("upgrade during cancellation", err)
	}
	grants, err := ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 0 {
		t.Fatal("reservation granted access", grants, err)
	}
}
