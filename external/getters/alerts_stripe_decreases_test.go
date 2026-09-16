package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	b := alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: "sub_" + id, CustomerID: "cus_" + id, PriceID: "price_" + id, ProductID: "prod_" + id, PaidSlots: 3}
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
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$2,next_reconcile_at=now()+interval '1 day' WHERE id=$1`, id, "cs_"+id); err != nil {
		t.Fatal(err)
	}
	if err = ProcessAlertsStripeDecrease(ctx, app, person, q.ID, alertsstripe.Client{}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("unconfirmed decrease processed", err)
	}
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
	var due bool
	if err = app.DB.QueryRow(ctx, `SELECT next_reconcile_at<=now() FROM alerts_stripe_checkouts WHERE id=$1`, id).Scan(&due); err != nil || !due {
		t.Fatal("confirmation did not wake worker", due, err)
	}
	workerCalls := 0
	_, err = reconcileDueAlertsStripeCheckouts(ctx, app, func(attempt context.Context, owner, checkoutID string) error {
		if checkoutID != id {
			return nil
		}
		workerCalls++
		var leased bool
		if e := app.DB.QueryRow(ctx, `SELECT next_reconcile_at>now()+interval '1 minute' AND next_reconcile_at<=now()+interval '2 minutes' FROM alerts_stripe_checkouts WHERE id=$1`, id).Scan(&leased); e != nil || !leased {
			t.Fatal("decrease worker lease", leased, e)
		}
		return nil
	})
	if err != nil || workerCalls != 1 {
		t.Fatal("confirmed decrease not queued", workerCalls, err)
	}
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
	if err = ProcessAlertsStripeDecrease(ctx, app, person, q.ID, alertsstripe.Client{}); err != nil {
		t.Fatal("scheduled decrease before renewal", err)
	}
	if err = ProcessAlertsStripeDecrease(ctx, app, uuid.NewString(), q.ID, alertsstripe.Client{}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign process", err)
	}
	t.Run("renewal transition preserves history", func(t *testing.T) {
		tx, e := app.DB.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); e != nil {
			t.Fatal(e)
		}
		next := b
		next.PaidSlots = 1
		observation := alertsstripe.DecreaseTransition{Binding: next, ScheduleID: schedule, EffectiveAt: preview.EffectiveAt, State: alertsstripe.SubscriptionState{State: "active", ProviderStatus: "active", PeriodStart: end.Unix(), PeriodEnd: end.AddDate(0, 1, 0).Unix()}}
		previous := alertsstripe.PaidPeriod{InvoiceID: "in_decrease_base_" + id, SubscriptionID: b.SubscriptionID, Quantity: 3, Start: start, End: end, PaidAt: start}
		if _, e = alertsStripeRenewalPreviousQuantity(ctx, tx, next, previous); !errors.Is(e, alertsstripe.ErrInvoice) {
			t.Fatal("unapplied decrease supplied grace", e)
		}
		if e = recordAlertsStripeDecreaseTransition(ctx, tx, *q, b, observation, now); !errors.Is(e, alertsstripe.ErrSubscription) {
			t.Fatal("early transition", e)
		}
		for i := 0; i < 2; i++ {
			if e = recordAlertsStripeDecreaseTransition(ctx, tx, *q, b, observation, end.Add(time.Second)); e != nil {
				t.Fatal(e)
			}
		}
		eligible, e := alertsStripeRenewalPreviousQuantity(ctx, tx, next, previous)
		if e != nil || eligible.Quantity != 1 || previous.Quantity != 3 || !eligible.End.Equal(previous.End) || eligible.InvoiceID != previous.InvoiceID {
			t.Fatal("decrease grace allowance", eligible, e)
		}
		for _, scenario := range []string{"different boundary", "insufficient prior payment", "different previous quantity", "foreign binding"} {
			p := previous
			bound := next
			switch scenario {
			case "different boundary":
				p.End = p.End.Add(time.Second)
			case "insufficient prior payment":
				p.Quantity = 0
			case "different previous quantity":
				p.Quantity = 4
			case "foreign binding":
				bound.PersonID = uuid.NewString()
			}
			if _, e = alertsStripeRenewalPreviousQuantity(ctx, tx, bound, p); !errors.Is(e, alertsstripe.ErrInvoice) {
				t.Fatal(scenario, e)
			}
		}
		var current, original int64
		invoiceID := "in_decreased_renewal_" + id
		invoiceJSON := fmt.Sprintf(`{"id":%q,"status":"open","currency":"usd","customer":%q,"billing_reason":"subscription_cycle","total":200,"amount_paid":0,"amount_remaining":200,"attempted":true,"attempt_count":1,"collection_method":"charge_automatically","automatic_tax":{"enabled":true,"status":"complete"},"parent":{"type":"subscription_details","subscription_details":{"subscription":%q,"metadata":{"product":"alerts.repository-slots","account_id":%q,"checkout_intent":%q}}},"lines":{"data":[{"currency":"usd","amount":200,"quantity":1,"period":{"start":%d,"end":%d},"parent":{"type":"subscription_item_details","subscription_item_details":{"subscription":%q,"proration":false}},"pricing":{"price_details":{"price":%q,"product":%q}}}]}}`, invoiceID, b.CustomerID, b.SubscriptionID, person, id, end.Unix(), end.AddDate(0, 1, 0).Unix(), b.SubscriptionID, b.PriceID, b.ProductID)
		var invoice stripe.Invoice
		if e = json.Unmarshal([]byte(invoiceJSON), &invoice); e != nil {
			t.Fatal(e)
		}
		price := &stripe.Price{ID: b.PriceID, Currency: "usd", UnitAmount: 200, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "month", IntervalCount: 1}, Product: &stripe.Product{ID: b.ProductID, TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
		client := alertsstripe.Client{Invoice: func(got string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
			if got != invoiceID {
				t.Fatal(got)
			}
			return &invoice, nil
		}, Price: func(got string, p *stripe.PriceParams) (*stripe.Price, error) {
			if got != b.PriceID {
				t.Fatal(got)
			}
			return price, nil
		}}
		failureState := observation.State
		failureState.State = "renewal_unpaid"
		failureState.ProviderStatus = "past_due"
		failureState.LatestInvoice = invoiceID
		for _, delay := range []time.Duration{time.Hour, 48 * time.Hour} {
			if e = projectAlertsStripeRenewal(ctx, tx, next, failureState, client, end.Add(delay)); e != nil {
				t.Fatal(e)
			}
		}
		var graceSlots int64
		var due time.Time
		if e = tx.QueryRow(ctx, `SELECT paid_slots,due_at FROM alerts_stripe_renewals WHERE invoice_id=$1`, invoiceID).Scan(&graceSlots, &due); e != nil || graceSlots != 1 || !due.Equal(end) {
			t.Fatal("decreased renewal grace", graceSlots, due, e)
		}
		var status string
		var transitions int
		if e = tx.QueryRow(ctx, `SELECT (c.binding->>'PaidSlots')::bigint,(o.binding->>'PaidSlots')::bigint,d.state FROM alerts_stripe_current_subscriptions c JOIN alerts_stripe_subscriptions o USING(checkout_id) JOIN alerts_stripe_decreases d USING(checkout_id) WHERE d.quote_id=$1`, q.ID).Scan(&current, &original, &status); e != nil || current != 1 || original != 3 || status != "applied" {
			t.Fatal(current, original, status, e)
		}
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM alerts_stripe_quantity_history WHERE decrease_quote_id=$1`, q.ID).Scan(&transitions); e != nil || transitions != 1 {
			t.Fatal(transitions, e)
		}
		var paidSlots int64
		if e = tx.QueryRow(ctx, `SELECT (paid_period->>'Quantity')::bigint FROM alerts_stripe_invoices WHERE checkout_id=$1`, id).Scan(&paidSlots); e != nil || paidSlots != 3 {
			t.Fatal("old invoice changed", paidSlots, e)
		}
		// Roll back this simulated future so the remaining conflict fixtures can
		// continue testing the actual current period and scheduled state.
		if e = tx.Rollback(ctx); e != nil {
			t.Fatal(e)
		}
	})
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
