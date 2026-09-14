package getters

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestOpenNodeServiceCreationClaimPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Local OpenNode claim test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "claim-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	start, end := time.Now().UTC(), time.Now().UTC().AddDate(1, 0, 0)
	purchase, err := CreateServicePurchase(app, person, "opennode-claim-test", ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Alerts paid repository", Provider: "opennode", Quantity: 1, UnitCents: 1200, TotalCents: 1200, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	const callers = 8
	results := make(chan *ServicePaymentAttempt, callers)
	errors := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			attempt, err := BeginServicePayment(app, person, purchase.OrderID)
			if err != nil {
				errors <- err
				return
			}
			results <- attempt
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var id string
	claims := 0
	for attempt := range results {
		if id != "" && attempt.ID != id {
			t.Fatal("duplicate payment attempt")
		}
		id = attempt.ID
		if attempt.CanCreateCharge {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("creation claims=%d want 1", claims)
	}
	// Simulate an ambiguous network result: no provider reference was saved.
	// A new DB transaction must still refuse another outbound creation.
	retry, err := BeginServicePayment(app, person, purchase.OrderID)
	if err != nil || retry.CanCreateCharge || retry.ID != id {
		t.Fatalf("ambiguous retry: %+v %v", retry, err)
	}
	if _, err := app.DB.Exec(ctx, `DELETE FROM service_charge_creation_claims WHERE attempt_id=$1`, id); err == nil {
		t.Fatal("creation claim could be erased")
	}
	if err := BindServicePayment(app, id, "opennode", "local-reconciled-"+id); err != nil {
		t.Fatal(err)
	}
	retry, err = BeginServicePayment(app, person, purchase.OrderID)
	if err != nil || retry.CanCreateCharge || retry.Reference != "local-reconciled-"+id {
		t.Fatalf("reconciled reference missing: %+v %v", retry, err)
	}
}
