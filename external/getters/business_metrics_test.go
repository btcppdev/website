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
		if count.Metric == "service_receipt_jobs" || count.Metric == "service_receipt_oldest_pending_timestamp_seconds" {
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
