package getters

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestPendingServiceSubscriptionActionsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	bound, unbound, other, closed := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{bound, unbound, other, closed} {
		product := "alerts.repository-slots"
		if id == other {
			product = "insider.membership.annual"
		}
		exec(`INSERT INTO service_stripe_checkouts(id,product_code,request_key,intent) VALUES($1,$2,$3,'{}')`, id, product, id)
	}
	exec(`UPDATE service_stripe_checkouts SET closed_at=now() WHERE id=$1`, closed)
	exec(`INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,'{}')`, bound, "sub_"+bound)
	increase, decrease, withdrawal, cancellation, resolved := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO alerts_stripe_increase_quotes(id,checkout_id,revision,preview,expires_at) VALUES($1,$2,1,'{}',now()+interval '1 hour')`, increase, bound)
	exec(`INSERT INTO alerts_stripe_increases(quote_id,checkout_id,state) VALUES($1,$2,'review')`, increase, bound)
	exec(`INSERT INTO alerts_stripe_increase_submissions(quote_id,invoice_id,lease_until) VALUES($1,$2,now())`, increase, "in_"+increase)
	exec(`INSERT INTO alerts_stripe_decrease_quotes(id,checkout_id,revision,preview,expires_at) VALUES($1,$2,1,'{}',now()+interval '1 hour')`, decrease, bound)
	exec(`INSERT INTO alerts_stripe_decreases(quote_id,checkout_id,state) VALUES($1,$2,'scheduled')`, decrease, bound)
	exec(`INSERT INTO alerts_stripe_decrease_attachments(quote_id,schedule_id,lease_until) VALUES($1,$2,now())`, decrease, "sub_sched_"+decrease)
	exec(`INSERT INTO alerts_stripe_decrease_schedules(quote_id,schedule_id,lease_until,snapshot,verified_at) VALUES($1,$2,now(),'{}',now())`, decrease, "sub_sched_"+decrease)
	exec(`INSERT INTO alerts_stripe_decrease_withdrawals(id,quote_id,state) VALUES($1,$2,'review')`, withdrawal, decrease)
	exec(`INSERT INTO service_stripe_cancellations(id,checkout_id,period_end) VALUES($1,$2,2000000000)`, cancellation, bound)
	exec(`INSERT INTO service_stripe_cancellations(id,checkout_id,period_end,state) VALUES($1,$2,2000000000,'confirmed')`, resolved, bound)
	want := map[string]string{unbound: "checkout", increase: "increase", decrease: "decrease", withdrawal: "withdrawal", cancellation: "cancellation"}
	excluded := map[string]bool{bound: true, other: true, closed: true, resolved: true}
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 10000 {
			t.Fatal("pagination did not terminate")
		}
		page, err := pagePendingServiceSubscriptionActions(ctx, app, "alerts", cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			key := item.Kind + ":" + item.ID
			if seen[key] || excluded[item.ID] {
				t.Fatal("duplicate or excluded action", item)
			}
			seen[key] = true
			if kind, ok := want[item.ID]; ok && (kind != item.Kind || item.ProductCode != "alerts.repository-slots") {
				t.Fatal("incorrect action", item)
			}
		}
		if page.Next == "" {
			break
		}
		if _, err := pagePendingServiceSubscriptionActions(ctx, app, "insider", page.Next, 2); err != ErrServiceRefundCursor {
			t.Fatal("cross-service cursor", err)
		}
		if _, err := PageUnsettledServicePayments(ctx, app, "alerts", page.Next); err != ErrServiceRefundCursor {
			t.Fatal("cross-report cursor", err)
		}
		cursor = page.Next
	}
	for id, kind := range want {
		if !seen[kind+":"+id] {
			t.Fatal("missing action", id, kind)
		}
	}
	if _, err := PagePendingServiceSubscriptionActions(ctx, app, "alerts", "invalid"); err != ErrServiceRefundCursor {
		t.Fatal("invalid cursor", err)
	}
	if _, err := pagePendingServiceSubscriptionActions(ctx, app, "alerts", "", 101); err != ErrServiceQuote {
		t.Fatal("unbounded page", err)
	}
}
