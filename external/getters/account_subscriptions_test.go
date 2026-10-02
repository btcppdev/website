package getters

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestAccountCardSubscriptionsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := app.DatabaseContext()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner, other := uuid.NewString(), uuid.NewString()
	for _, person := range []string{owner, other} {
		exec(`INSERT INTO people(id,name) VALUES($1,'Subscription fixture')`, person)
	}
	t.Cleanup(func() { exec(`DELETE FROM people WHERE id IN ($1,$2)`, owner, other) })
	for i, fixture := range []struct {
		person, product, state string
		bound                  bool
	}{
		{owner, "alerts.repository-slots", "cancelled", true},
		{owner, "alerts.repository-slots", "active", true},
		{owner, "insider.membership.annual", "cancelling", true},
		{other, "alerts.repository-slots", "disputed", true},
		{owner, "alerts.repository-slots", "", false},
	} {
		id := uuid.NewString()
		exec(`INSERT INTO service_stripe_checkouts(id,person_id,product_code,request_key,intent,created_at) VALUES($1,$2,$3,$4,'{}',$5)`, id, fixture.person, fixture.product, id, time.Now().Add(time.Duration(i)*time.Minute))
		exec(`UPDATE service_stripe_checkouts SET closed_at=now() WHERE id=$1`, id)
		if !fixture.bound {
			continue
		}
		exec(`INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,'{}')`, id, "sub_"+id)
		exec(`INSERT INTO service_stripe_subscription_state(checkout_id,revision,snapshot,observed_at) VALUES($1,1,jsonb_build_object('state',$2::text),now())`, id, fixture.state)
	}
	got, err := AccountCardSubscriptions(app, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Service != "alerts" || got[0].State.State != "active" || got[1].Service != "insider" || got[1].State.State != "cancelling" {
		t.Fatalf("wrong account/latest bound subscription: %+v", got)
	}
	got, err = AccountCardSubscriptions(app, uuid.NewString())
	if err != nil || len(got) != 0 {
		t.Fatalf("unknown account: %+v %v", got, err)
	}
	exec(`UPDATE people SET is_deleted_account=true WHERE id=$1`, owner)
	got, err = AccountCardSubscriptions(app, owner)
	if err != nil || len(got) != 0 {
		t.Fatalf("deleted account: %+v %v", got, err)
	}
}
