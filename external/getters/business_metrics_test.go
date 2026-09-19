package getters

import (
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
		if strings.HasPrefix(count.Metric, "account_deletion_") || count.Metric == "service_refund_jobs" || count.Metric == "service_refund_oldest_pending_timestamp_seconds" || count.Metric == "service_fulfillment_jobs" || count.Metric == "service_fulfillment_oldest_pending_timestamp_seconds" || count.Metric == "service_receipt_jobs" || count.Metric == "service_receipt_oldest_pending_timestamp_seconds" {
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
