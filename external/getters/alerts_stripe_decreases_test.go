package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
	"sync"
	"testing"
	"time"
)

func TestAlertsStripeDecreaseReservationPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Decrease reservation') RETURNING id::text`).Scan(&person); err != nil {
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
	checkout, err := ReserveAlertsStripeCheckout(app, person, "decrease-reservation-key", 3, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	id := checkout.Intent.ID
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Hour)
	end := start.AddDate(0, 1, 0)
	b := alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, PaidSlots: 3}
	raw, _ := json.Marshal(b)
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, id, b.SubscriptionID, raw); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(alertsstripe.SubscriptionState{State: "active", ProviderStatus: "active", PeriodStart: start.Unix(), PeriodEnd: end.Unix()})
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscription_state(checkout_id,revision,snapshot) VALUES($1,1,$2)`, id, state); err != nil {
		t.Fatal(err)
	}
	if _, err = recordAlertsStripeInvoice(app, b, alertsstripe.PaidPeriod{InvoiceID: "in_decrease_base_" + id, SubscriptionID: b.SubscriptionID, Quantity: 3, TotalCents: 600, Start: start, End: end, PaidAt: start}); err != nil {
		t.Fatal(err)
	}
	preview := alertsstripe.DecreasePreview{PreviousSlots: 3, PaidSlots: 1, MonthlyCents: 200, PeriodStart: start.Unix(), EffectiveAt: end.Unix(), ExpiresAt: now.Add(10 * time.Minute)}
	q, err := SaveAlertsStripeDecreaseQuote(ctx, app, person, id, 1, preview)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SaveAlertsStripeDecreaseQuote(ctx, app, person, id, 1, preview)
	if err != nil {
		t.Fatal(err)
	}
	up, err := SaveAlertsStripeIncreaseQuote(ctx, app, person, id, 1, alertsstripe.IncreasePreview{SubscriptionItemID: "si_test", PreviousSlots: 3, PaidSlots: 4, PeriodStart: start.Unix(), PeriodEnd: end.Unix(), ProrationDate: now.Unix(), TotalCents: 100, ExpiresAt: now.Add(10 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = SaveAlertsStripeDecreaseQuote(ctx, app, person, id, 99, preview); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale review", err)
	}
	if _, err = ReserveAlertsStripeDecrease(ctx, app, uuid.NewString(), q.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign confirmation", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_quotes SET preview='{}' WHERE id=$1`, q.ID); err == nil {
		t.Fatal("quote mutable")
	}
	expired := preview
	expired.ExpiresAt = now.Add(-time.Second)
	if _, err = SaveAlertsStripeDecreaseQuote(ctx, app, person, id, 1, expired); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("expired review saved", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=2 WHERE checkout_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeDecrease(ctx, app, person, q.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("stale confirmation", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=1 WHERE checkout_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := ReserveAlertsStripeDecrease(ctx, app, person, q.ID)
			if e != nil || s != "pending" {
				t.Errorf("confirmation %s %v", s, e)
			}
		}()
	}
	wg.Wait()
	if _, err = claimAlertsStripeDecreaseAttachment(ctx, app, uuid.NewString(), q.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign attachment", err)
	}
	claims := make(chan *alertsDecreaseAttachmentClaim, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, e := claimAlertsStripeDecreaseAttachment(ctx, app, person, q.ID)
			if e == nil {
				claims <- claim
			} else if !errors.Is(e, alertsstripe.ErrReconcile) {
				t.Errorf("attachment claim: %v", e)
			}
		}()
	}
	wg.Wait()
	close(claims)
	if len(claims) != 1 {
		t.Fatalf("concurrent claims: %d", len(claims))
	}
	claim := <-claims
	if claim.Attempt != 1 || claim.Binding.PaidSlots != 3 {
		t.Fatal(claim)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_attachments SET lease_until=now()-interval '1 second' WHERE quote_id=$1`, q.ID); err != nil {
		t.Fatal(err)
	}
	claim, err = claimAlertsStripeDecreaseAttachment(ctx, app, person, q.ID)
	if err != nil || claim.Attempt != 2 {
		t.Fatal(claim, err)
	}
	stale, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_attachments SET schedule_id='sub_sched_stale' WHERE quote_id=$1 AND attempts=1`, q.ID)
	if err != nil || stale.RowsAffected() != 0 {
		t.Fatal("late response accepted", err)
	}
	schedule := "sub_sched_" + q.ID
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_attachments SET schedule_id=$2 WHERE quote_id=$1 AND attempts=2`, q.ID, schedule); err != nil {
		t.Fatal(err)
	}
	// Saved references bypass provider creation even while the lease is active.
	got, err := AttachAlertsStripeDecrease(ctx, app, person, q.ID, alertsstripe.Client{})
	if err != nil || got != schedule {
		t.Fatal(got, err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_attachments SET schedule_id='sub_sched_other' WHERE quote_id=$1`, q.ID); err == nil {
		t.Fatal("schedule reference mutable")
	}
	if _, err = app.DB.Exec(ctx, `DELETE FROM alerts_stripe_decrease_attachments WHERE quote_id=$1`, q.ID); err == nil {
		t.Fatal("attachment evidence deleted")
	}
	if _, err = ReserveAlertsStripeDecrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("overlapping decrease", err)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, up.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("upgrade during decrease", err)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, id, uuid.NewString(), 1, end.Unix()); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("cancellation during decrease", err)
	}
	grants, err := ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 3 || !grants[0].EndsAt.Equal(end) {
		t.Fatal("current paid allowance changed", grants, err)
	}
	if _, err = claimAlertsStripeDecreaseSchedule(ctx, app, uuid.NewString(), q.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign schedule", err)
	}
	phaseClaim, err := claimAlertsStripeDecreaseSchedule(ctx, app, person, q.ID)
	if err != nil || phaseClaim.Attempt != 1 || phaseClaim.Complete {
		t.Fatal(phaseClaim, err)
	}
	if _, err = claimAlertsStripeDecreaseSchedule(ctx, app, person, q.ID); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("phase lease bypassed", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_schedules SET lease_until=now()-interval '1 second' WHERE quote_id=$1`, q.ID); err != nil {
		t.Fatal(err)
	}
	newClaim, err := claimAlertsStripeDecreaseSchedule(ctx, app, person, q.ID)
	if err != nil || newClaim.Attempt != 2 {
		t.Fatal(newClaim, err)
	}
	provider := &stripe.SubscriptionSchedule{ID: schedule, Status: "active", EndBehavior: "release", Customer: &stripe.Customer{ID: b.CustomerID}, Subscription: &stripe.Subscription{ID: b.SubscriptionID}, CurrentPhase: &stripe.SubscriptionScheduleCurrentPhase{StartDate: preview.PeriodStart, EndDate: preview.EffectiveAt}, Metadata: map[string]string{"alerts_decrease": q.ID, "account_id": person, "checkout_intent": id}}
	method := stripe.SubscriptionCollectionMethod("charge_automatically")
	for _, quantity := range []int64{3, 1} {
		provider.Phases = append(provider.Phases, &stripe.SubscriptionSchedulePhase{Currency: "usd", CollectionMethod: &method, AutomaticTax: &stripe.SubscriptionAutomaticTax{Enabled: true}, ProrationBehavior: "none", Metadata: map[string]string{"account_id": person, "checkout_intent": id, "product": "alerts.repository-slots"}, Items: []*stripe.SubscriptionSchedulePhaseItem{{Price: &stripe.Price{ID: b.PriceID}, Quantity: quantity}}})
	}
	provider.Phases[0].StartDate = preview.PeriodStart
	provider.Phases[0].EndDate = preview.EffectiveAt
	provider.Phases[1].StartDate = preview.EffectiveAt
	provider.Phases[1].EndDate = end.AddDate(0, 1, 0).Unix()
	if err = recordAlertsStripeDecreaseSchedule(ctx, app, phaseClaim, provider); !errors.Is(err, alertsstripe.ErrReconcile) {
		t.Fatal("stale phase response", err)
	}
	provider.Phases[1].Items[0].Quantity = 99
	if err = recordAlertsStripeDecreaseSchedule(ctx, app, newClaim, provider); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("bad provider phase", err)
	}
	provider.Phases[1].Items[0].Quantity = 1
	if err = recordAlertsStripeDecreaseSchedule(ctx, app, newClaim, provider); err != nil {
		t.Fatal(err)
	}
	if err = recordAlertsStripeDecreaseSchedule(ctx, app, newClaim, provider); err != nil {
		t.Fatal("repeat evidence", err)
	}
	if err = ScheduleAlertsStripeDecrease(ctx, app, person, q.ID, alertsstripe.Client{}); err != nil {
		t.Fatal("scheduled retry called provider", err)
	}
	var revision int64
	if err = app.DB.QueryRow(ctx, `SELECT revision FROM alerts_stripe_subscription_state WHERE checkout_id=$1`, id).Scan(&revision); err != nil || revision != 2 {
		t.Fatal("revision changed more than once", revision, err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decrease_schedules SET snapshot='{}' WHERE quote_id=$1`, q.ID); err == nil {
		t.Fatal("verified schedule mutable")
	}
	grants, err = ActiveServiceEntitlements(app, person, now)
	if err != nil || len(grants) != 1 || grants[0].Quantity != 3 || !grants[0].EndsAt.Equal(end) {
		t.Fatal("scheduling changed access", grants, err)
	}
	if status, e := ReserveAlertsStripeDecrease(ctx, app, person, q.ID); e != nil || status != "scheduled" {
		t.Fatal("scheduled retry", status, e)
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, up.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("scheduled decrease overwritten", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='cancelled' WHERE quote_id=$1`, q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='pending' WHERE quote_id=$1`, q.ID); err == nil {
		t.Fatal("resolved decrease reopened")
	}
	// Subsequent conflict fixtures use the original revision after this resolved
	// schedule; production refreshes would issue new reviews at the new revision.
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=1 WHERE checkout_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for _, attempted := range []bool{false, true} {
		// Persist historical evidence directly to exercise expiry without sleeps.
		historical := preview
		historical.ExpiresAt = now.Add(-time.Minute)
		encoded, _ := json.Marshal(historical)
		historyID := uuid.NewString()
		if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_decrease_quotes(id,checkout_id,revision,preview,created_at,expires_at) VALUES($1,$2,1,$3,$4,$5)`, historyID, id, encoded, now.Add(-10*time.Minute), historical.ExpiresAt); err != nil {
			t.Fatal(err)
		}
		if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_decreases(quote_id,checkout_id,created_at) VALUES($1,$2,$3)`, historyID, id, now.Add(-5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if attempted {
			if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_decrease_attachments(quote_id,first_attempt_at,lease_until) VALUES($1,$2,$3)`, historyID, now.Add(-5*time.Minute), now.Add(-3*time.Minute)); err != nil {
				t.Fatal(err)
			}
			// A harmless provider refresh can advance the revision after consent.
			if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=2 WHERE checkout_id=$1`, id); err != nil {
				t.Fatal(err)
			}
		}
		historyClaim, e := claimAlertsStripeDecreaseAttachment(ctx, app, person, historyID)
		var status string
		if err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_decreases WHERE quote_id=$1`, historyID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if attempted {
			if e != nil || historyClaim.Attempt != 2 || status != "pending" || !historyClaim.FirstAttempt.Equal(now.Add(-5*time.Minute)) {
				t.Fatal(historyClaim, status, e)
			}
			if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_decreases SET state='cancelled' WHERE quote_id=$1`, historyID); err != nil {
				t.Fatal(err)
			}
			if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=1 WHERE checkout_id=$1`, id); err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(e, alertsstripe.ErrReconcile) || status != "expired" {
			t.Fatal(status, e)
		}
	}
	if _, err = ReserveAlertsStripeIncrease(ctx, app, person, up.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeDecrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("decrease during upgrade", err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_increases SET state='expired' WHERE quote_id=$1`, up.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeCancellation(ctx, app, person, id, uuid.NewString(), 1, end.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveAlertsStripeDecrease(ctx, app, person, second.ID); !errors.Is(err, alertsstripe.ErrSubscription) {
		t.Fatal("decrease during cancellation", err)
	}
}
