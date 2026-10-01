package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestDatabaseSmokeBusinessMetrics(t *testing.T) {
	ctx := databaseSmokeContext(t)
	counts, err := ListBusinessMetricCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{
		"tickets": true, "ticket_checkins": true, "speaker_applications": true,
		"volunteer_applications": true, "recording_broadcasts": true,
	}
	for _, count := range counts {
		if strings.HasPrefix(count.Metric, "service_refund_tax_") || strings.HasPrefix(count.Metric, "alerts_checkout_") || strings.HasPrefix(count.Metric, "account_deletion_") || count.Metric == "service_refund_jobs" || count.Metric == "service_refund_oldest_pending_timestamp_seconds" || count.Metric == "service_fulfillment_jobs" || count.Metric == "service_fulfillment_oldest_pending_timestamp_seconds" || count.Metric == "service_receipt_jobs" || count.Metric == "service_receipt_oldest_pending_timestamp_seconds" {
			if count.Conference != "" || count.Count < 0 {
				t.Fatalf("invalid receipt aggregate: %+v", count)
			}
			continue
		}
		if !known[count.Metric] || count.Conference == "" || count.Count < 0 {
			t.Fatalf("invalid business metric row: %+v", count)
		}
	}
}

func TestServiceFulfillmentMetricsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	counts, err := ListBusinessMetricCounts(app)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, metric := range counts {
		var want float64
		if metric.Metric == "service_fulfillment_jobs" {
			if metric.State != "pending" && metric.State != "accepted" && metric.State != "review" {
				t.Fatal("unbounded state", metric)
			}
			if err := app.DB.QueryRow(app.DatabaseContext(), `SELECT count(*)::float8 FROM service_fulfillment_jobs WHERE state=$1`, metric.State).Scan(&want); err != nil {
				t.Fatal(err)
			}
			found[metric.State] = true
		} else if metric.Metric == "service_fulfillment_oldest_pending_timestamp_seconds" {
			if err := app.DB.QueryRow(app.DatabaseContext(), `SELECT coalesce(extract(epoch FROM min(created_at)),0)::float8 FROM service_fulfillment_jobs WHERE state='pending'`).Scan(&want); err != nil {
				t.Fatal(err)
			}
			found["oldest"] = true
		} else {
			continue
		}
		if metric.Count != want || metric.Conference != "" {
			t.Fatal("incorrect/private aggregate", metric, want)
		}
	}
	if len(found) != 4 {
		t.Fatal("missing fulfillment metrics", found)
	}
}

func TestServiceRefundMetricsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	counts, err := ListBusinessMetricCounts(app)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, metric := range counts {
		var want float64
		if metric.Metric == "service_refund_jobs" {
			if metric.State != "pending" && metric.State != "accepted" && metric.State != "review" {
				t.Fatal("unbounded state", metric)
			}
			if err := app.DB.QueryRow(app.DatabaseContext(), `SELECT count(*)::float8 FROM service_refund_jobs WHERE state=$1`, metric.State).Scan(&want); err != nil {
				t.Fatal(err)
			}
			found[metric.State] = true
		} else if metric.Metric == "service_refund_oldest_pending_timestamp_seconds" {
			if err := app.DB.QueryRow(app.DatabaseContext(), `SELECT coalesce(extract(epoch FROM min(created_at)),0)::float8 FROM service_refund_jobs WHERE state='pending'`).Scan(&want); err != nil {
				t.Fatal(err)
			}
			found["oldest"] = true
		} else {
			continue
		}
		if metric.Count != want || metric.Conference != "" {
			t.Fatal("incorrect/private aggregate", metric, want)
		}
	}
	if len(found) != 4 {
		t.Fatal("missing refund metrics", found)
	}
}

func TestAccountDeletionMetricsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	counts, err := ListBusinessMetricCounts(app)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"account_deletion_delivery_jobs/pending":                          `SELECT count(*)::float8 FROM account_deletion_deliveries WHERE state='pending'`,
		"account_deletion_delivery_jobs/accepted":                         `SELECT count(*)::float8 FROM account_deletion_deliveries WHERE state='accepted'`,
		"account_deletion_cancellation_jobs/pending":                      `SELECT count(*)::float8 FROM account_deletion_cancellations WHERE state='pending'`,
		"account_deletion_cancellation_jobs/confirmed":                    `SELECT count(*)::float8 FROM account_deletion_cancellations WHERE state='confirmed'`,
		"account_deletion_cancellation_jobs/review":                       `SELECT count(*)::float8 FROM account_deletion_cancellations WHERE state='review'`,
		"account_deletion_delivery_oldest_pending_timestamp_seconds/":     `SELECT coalesce(extract(epoch FROM min(created_at)),0)::float8 FROM account_deletion_deliveries WHERE state='pending'`,
		"account_deletion_cancellation_oldest_pending_timestamp_seconds/": `SELECT coalesce(extract(epoch FROM min(requested_at)),0)::float8 FROM account_deletion_cancellations WHERE state='pending'`,
	}
	for _, metric := range counts {
		if !strings.HasPrefix(metric.Metric, "account_deletion_") {
			continue
		}
		key := metric.Metric + "/" + metric.State
		query, ok := expected[key]
		if !ok {
			t.Fatal("unexpected deletion metric", metric)
		}
		var want float64
		if err := app.DB.QueryRow(app.DatabaseContext(), query).Scan(&want); err != nil {
			t.Fatal(err)
		}
		if metric.Count != want || metric.Conference != "" {
			t.Fatal("incorrect or private deletion metric", metric, want)
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		t.Fatal("missing deletion metrics", expected)
	}
}

func TestAlertsCheckoutRecoveryMetricsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	snapshot := func() map[string]float64 {
		t.Helper()
		rows, err := ListBusinessMetricCounts(app)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]float64{}
		for _, r := range rows {
			if strings.HasPrefix(r.Metric, "alerts_checkout_") {
				if r.Conference != "" {
					t.Fatal("identity label", r)
				}
				out[r.Metric+":"+r.State] = r.Count
			}
		}
		return out
	}
	before := snapshot()
	person := uuid.NewString()
	if _, err := app.DB.Exec(ctx, `INSERT INTO people(id,name) VALUES($1,'Checkout metrics fixture')`, person); err != nil {
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
	checkout, err := ReserveAlertsStripeCheckout(app, person, uuid.NewString(), 1, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	key := "alerts_checkout_recovery_jobs:"
	current := snapshot()
	if current[key+"missing_reference"] != before[key+"missing_reference"]+1 || current["alerts_checkout_oldest_unresolved_timestamp_seconds:"] <= 0 || current["alerts_checkout_oldest_due_timestamp_seconds:"] <= 0 {
		t.Fatal("lost reference invisible", current)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$2 WHERE id=$1`, checkout.Intent.ID, "cs_metrics_"+checkout.Intent.ID); err != nil {
		t.Fatal(err)
	}
	current = snapshot()
	if current[key+"missing_reference"] != before[key+"missing_reference"] || current[key+"referenced"] != before[key+"referenced"]+1 {
		t.Fatal("reference discovery not reflected", current)
	}
	b := alertsstripe.Binding{IntentID: checkout.Intent.ID, PersonID: person, SubscriptionID: "sub_metrics_" + checkout.Intent.ID, CustomerID: "cus_metrics", PriceID: "price_metrics", ProductID: "prod_metrics", PaidSlots: 1}
	raw, _ := json.Marshal(b)
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, checkout.Intent.ID, b.SubscriptionID, raw); err != nil {
		t.Fatal(err)
	}
	current = snapshot()
	if current[key+"referenced"] != before[key+"referenced"] || current[key+"bound"] != before[key+"bound"]+1 {
		t.Fatal("bound checkout misclassified", current)
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_checkouts SET closed_at=now() WHERE id=$1`, checkout.Intent.ID); err != nil {
		t.Fatal(err)
	}
	current = snapshot()
	for _, state := range []string{"missing_reference", "referenced", "bound"} {
		if current[key+state] != before[key+state] {
			t.Fatal("closed checkout counted", current)
		}
	}
}

func TestServiceRefundTaxMetricsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	counts, err := ListBusinessMetricCounts(app)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, metric := range counts {
		var want float64
		if metric.Metric == "service_refund_tax_jobs" {
			if metric.State != "pending" && metric.State != "accepted" && metric.State != "review" {
				t.Fatal("unbounded state", metric)
			}
			if err := app.DB.QueryRow(app.DatabaseContext(), `SELECT count(*)::float8 FROM service_refund_tax_jobs WHERE state=$1`, metric.State).Scan(&want); err != nil {
				t.Fatal(err)
			}
			found[metric.State] = true
		} else if metric.Metric == "service_refund_tax_oldest_pending_timestamp_seconds" {
			if err := app.DB.QueryRow(app.DatabaseContext(), `SELECT coalesce(extract(epoch FROM min(created_at)),0)::float8 FROM service_refund_tax_jobs WHERE state='pending'`).Scan(&want); err != nil {
				t.Fatal(err)
			}
			found["oldest"] = true
		} else {
			continue
		}
		if metric.Count != want || metric.Conference != "" {
			t.Fatal("incorrect/private aggregate", metric, want)
		}
	}
	if len(found) != 4 {
		t.Fatal("missing refund metrics", found)
	}
}
