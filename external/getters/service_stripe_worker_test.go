package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/servicestripe"
	"github.com/google/uuid"
)

func TestServiceStripeRecoverySchedulePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var people, ids []string
	defer func() {
		for _, person := range people {
			if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < 6; i++ {
		var person string
		if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Stripe recovery queue fixture') RETURNING id::text`).Scan(&person); err != nil {
			t.Fatal(err)
		}
		people = append(people, person)
		if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
			t.Fatal(err)
		}
		stored, err := ReserveServiceStripeCheckout(app, person, uuid.NewString(), servicestripe.CheckoutIntent{ProductCode: "insider.membership.monthly", Quantity: 1, TaxCode: "txcd_test", ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/account/success", CancelURL: "https://insider.btcpp.dev/account/cancel"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, stored.Intent.ID)
		if i < 3 || i == 4 {
			if _, err = app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET stripe_session_id=$2 WHERE id=$1`, stored.Intent.ID, "cs_"+stored.Intent.ID); err != nil {
				t.Fatal(err)
			}
		}
		if i == 5 {
			if err := markServiceStripeCheckoutStarted(ctx, app, person, stored.Intent.ID, stored.Intent.ProductCode); err != nil {
				t.Fatal(err)
			}
		}
		if i == 2 {
			if _, err = app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET closed_at=now() WHERE id=$1`, stored.Intent.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, people[4]); err != nil {
		t.Fatal(err)
	}
	alert, err := ReserveAlertsStripeCheckout(app, people[0], uuid.NewString(), 1, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$2 WHERE id=$1`, alert.Intent.ID, "cs_"+alert.Intent.ID); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	recover := func(attempt context.Context, person, id, product string) error {
		if product != "insider.membership.monthly" {
			t.Error("cross-product recovery", product)
		}
		mu.Lock()
		seen[id]++
		mu.Unlock()
		deadline, ok := attempt.Deadline()
		if !ok || time.Until(deadline) > time.Minute {
			t.Error("missing bounded attempt")
		}
		var scheduled bool
		if err := app.DB.QueryRow(ctx, `SELECT next_reconcile_at>now()+interval '14 minutes' FROM service_stripe_checkouts WHERE id=$1`, id).Scan(&scheduled); err != nil || !scheduled {
			t.Error("provider called before durable lease", scheduled, err)
		}
		return errors.New("provider unavailable")
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = reconcileDueServiceStripeCheckouts(ctx, app, recover) }()
	}
	wg.Wait()
	if seen[ids[0]] != 1 || seen[ids[1]] != 1 || seen[ids[2]] != 0 || seen[ids[3]] != 0 || seen[ids[4]] != 0 || seen[ids[5]] != 1 || seen[alert.Intent.ID] != 0 {
		t.Fatal("duplicate or ineligible work", seen)
	}
	if _, err := reconcileDueServiceStripeCheckouts(ctx, app, recover); err != nil {
		t.Fatal(err)
	}
	if seen[ids[0]] != 1 || seen[ids[1]] != 1 {
		t.Fatal("retry before due time", seen)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET next_reconcile_at=now()-interval '1 second' WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := reconcileDueServiceStripeCheckouts(ctx, app, recover); err == nil {
		t.Fatal("provider failure not reported")
	}
	if seen[ids[0]] != 2 || seen[ids[1]] != 1 {
		t.Fatal("due work did not recover", seen)
	}
}
