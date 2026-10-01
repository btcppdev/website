package getters

import (
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	want := map[string]string{bound: "subscription", unbound: "checkout", increase: "increase", decrease: "decrease", withdrawal: "withdrawal", cancellation: "cancellation"}
	excluded := map[string]bool{other: true, closed: true, resolved: true}
	insiderWant := map[string]bool{}
	observed := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		for _, tc := range []struct {
			name    string
			state   servicestripe.SubscriptionState
			include bool
		}{
			{"unknown-scheduled", servicestripe.SubscriptionState{CancelAtPeriodEnd: true, PeriodEnd: 2000000000}, true},
			{"active", servicestripe.SubscriptionState{ProviderStatus: "active"}, true},
			{"unpaid", servicestripe.SubscriptionState{ProviderStatus: "unpaid", State: "renewal_unpaid"}, true},
			{"paused", servicestripe.SubscriptionState{ProviderStatus: "paused"}, true},
			{"canceled-failed", servicestripe.SubscriptionState{ProviderStatus: "canceled", State: "renewal_unpaid"}, false},
			{"expired", servicestripe.SubscriptionState{ProviderStatus: "incomplete_expired"}, false},
			{"scheduled", servicestripe.SubscriptionState{ProviderStatus: "active", CancelAtPeriodEnd: true, PeriodEnd: 2000000000}, false},
			{"explicit-end", servicestripe.SubscriptionState{ProviderStatus: "active", CancelAtPeriodEnd: true, CancelAt: 2000000000, PeriodEnd: 2000000000}, false},
			{"conflicting-end", servicestripe.SubscriptionState{ProviderStatus: "active", CancelAtPeriodEnd: true, CancelAt: 2000000001, PeriodEnd: 2000000000}, true},
		} {
			id := uuid.NewString()
			exec(`INSERT INTO service_stripe_checkouts(id,product_code,request_key,intent) VALUES($1,$2,$3,'{}')`, id, product, id)
			exec(`INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,'{}')`, id, "sub_"+id)
			raw, err := json.Marshal(tc.state)
			if err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO service_stripe_subscription_state(checkout_id,revision,snapshot,observed_at) VALUES($1,1,$2,$3)`, id, raw, observed)
			if tc.name == "active" {
				exec(`INSERT INTO service_stripe_cancellations(id,checkout_id,period_end,state) VALUES($1,$2,2000000000,'confirmed')`, uuid.NewString(), id)
			}
			if product == "insider.membership.annual" {
				insiderWant[id] = tc.include
			}
			if tc.include && product == "alerts.repository-slots" {
				want[id] = "subscription"
			} else {
				excluded[id] = true
			}
		}
	}
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
			if item.Kind == "subscription" {
				if item.ID == bound {
					if item.State != "unknown" || item.ObservedAt != nil {
						t.Fatal("missing observation misrepresented", item)
					}
				} else if _, ok := want[item.ID]; ok && (item.ObservedAt == nil || !item.ObservedAt.Equal(observed)) {
					t.Fatal("observation age omitted", item)
				}
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
	insiderSeen := map[string]bool{}
	for cursor, pages := "", 0; ; pages++ {
		if pages > 10000 {
			t.Fatal("Insider pagination did not terminate")
		}
		page, err := pagePendingServiceSubscriptionActions(ctx, app, "insider", cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if !strings.HasPrefix(item.ProductCode, "insider.") {
				t.Fatal("cross-service item", item)
			}
			if include, ok := insiderWant[item.ID]; ok {
				if !include || insiderSeen[item.ID] || item.Kind != "subscription" || item.ObservedAt == nil || !item.ObservedAt.Equal(observed) {
					t.Fatal("incorrect Insider subscription", item)
				}
				insiderSeen[item.ID] = true
			}
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	for id, include := range insiderWant {
		if include != insiderSeen[id] {
			t.Fatal("missing Insider subscription", id)
		}
	}
	if _, err := PagePendingServiceSubscriptionActions(ctx, app, "alerts", "invalid"); err != ErrServiceRefundCursor {
		t.Fatal("invalid cursor", err)
	}
	if _, err := pagePendingServiceSubscriptionActions(ctx, app, "alerts", "", 101); err != ErrServiceQuote {
		t.Fatal("unbounded page", err)
	}
}
