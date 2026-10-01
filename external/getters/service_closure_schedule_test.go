package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestServiceClosurePlanPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	actor := uuid.NewString()
	if _, err := app.DB.Exec(ctx, `INSERT INTO people(id,name) VALUES($1,'Closure planning fixture')`, actor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `TRUNCATE service_closure_plans,service_sales_closures`); err != nil {
			t.Error(err)
		}
	})
	at := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
	if err := RecordServiceClosurePlan(ctx, app, actor, "alerts", at, "Closure"); !errors.Is(err, ErrServiceRefundAdmin) {
		t.Fatal("nonadmin", err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor); err != nil {
		t.Fatal(err)
	}
	if err := RecordServiceClosurePlan(ctx, app, actor, "alerts", at, "Closure"); err == nil {
		t.Fatal("planned before sales stop")
	}
	if err := StopServiceSales(ctx, app, actor, "alerts", "Closure"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []time.Time{time.Time{}, at.Add(time.Nanosecond), at.Add(-2 * time.Hour)} {
		if err := RecordServiceClosurePlan(ctx, app, actor, "alerts", bad, "Closure"); !errors.Is(err, ErrServiceQuote) {
			t.Fatal("invalid time", err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := RecordServiceClosurePlan(ctx, app, actor, "alerts", at, "Closure"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	plan, err := GetServiceClosurePlan(ctx, app, "alerts")
	if err != nil || plan == nil || plan.ActorID != actor || !plan.ClosesAt.Equal(at) || plan.Reason != "Closure" || plan.RecordedAt.IsZero() {
		t.Fatal(plan, err)
	}
	if err := RecordServiceClosurePlan(ctx, app, actor, "alerts", at.Add(time.Hour), "Closure"); !errors.Is(err, ErrServiceQuote) {
		t.Fatal("changed replay", err)
	}
	if err := RecordServiceClosurePlan(ctx, app, actor, "alerts", at, "New reason"); !errors.Is(err, ErrServiceQuote) {
		t.Fatal("changed reason", err)
	}
	if other, err := GetServiceClosurePlan(ctx, app, "insider"); err != nil || other != nil {
		t.Fatal("other service affected", other, err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_closure_plans SET closes_at=closes_at+interval '1 hour' WHERE service='alerts'`); err == nil {
		t.Fatal("mutable plan")
	}
	if _, err := app.DB.Exec(ctx, `DELETE FROM service_closure_plans WHERE service='alerts'`); err == nil {
		t.Fatal("deleted plan")
	}
	if _, err := app.DB.Exec(ctx, `UPDATE people SET is_deleted_account=true WHERE id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if err := RecordServiceClosurePlan(ctx, app, actor, "alerts", at, "Closure"); !errors.Is(err, ErrServiceRefundAdmin) {
		t.Fatal("deleted admin", err)
	}
}
