package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

func TestServiceRecoverySchedulePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Local recovery scheduling test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "recovery-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	refs := map[string]bool{}
	for i := 0; i < 3; i++ {
		quote := ServiceQuote{ProductCode: "insider.article", ResourceID: "recovery", Name: "Recovery test", Provider: "opennode", Quantity: 1, UnitCents: 100, TotalCents: 100, ExpiresAt: time.Now().Add(time.Hour)}
		purchase, err := CreateServicePurchase(app, person, uuid.NewString(), quote)
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := BeginServicePayment(app, person, purchase.OrderID)
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			ref := uuid.NewString()
			refs[ref] = true
			if err := BindServicePayment(app, attempt.ID, "opennode", ref); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := app.DB.Exec(ctx, `UPDATE service_payment_attempts SET next_reconcile_at='1970-01-01' WHERE id=$1`, attempt.ID); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[string]int{}
	verify := func(_ context.Context, _ *config.AppContext, ref string) (bool, error) {
		mu.Lock()
		seen[ref]++
		mu.Unlock()
		return false, errors.New("simulated provider outage")
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = reconcileDueOpenNodeServices(ctx, app, verify) }()
	}
	wg.Wait()
	for ref := range refs {
		if seen[ref] != 1 {
			t.Fatalf("known charge checked %d times", seen[ref])
		}
	}
	var scheduled, unknown int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE a.provider_reference IS NOT NULL AND a.next_reconcile_at>now()+interval '14 minutes'),count(*) FILTER(WHERE a.provider_reference IS NULL AND a.next_reconcile_at<now()) FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE r.person_id=$1`, person).Scan(&scheduled, &unknown); err != nil || scheduled != 2 || unknown != 1 {
		t.Fatal("retry schedule or unknown charge handling", scheduled, unknown, err)
	}
	// Another run immediately after a failure cannot hammer those provider IDs.
	_, _ = reconcileDueOpenNodeServices(ctx, app, verify)
	for ref := range refs {
		if seen[ref] != 1 {
			t.Fatal("retried before durable deadline")
		}
	}
}
