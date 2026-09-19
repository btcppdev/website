package getters

import "testing"

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
		if count.Metric == "service_refund_jobs" || count.Metric == "service_refund_oldest_pending_timestamp_seconds" || count.Metric == "service_fulfillment_jobs" || count.Metric == "service_fulfillment_oldest_pending_timestamp_seconds" || count.Metric == "service_receipt_jobs" || count.Metric == "service_receipt_oldest_pending_timestamp_seconds" {
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
