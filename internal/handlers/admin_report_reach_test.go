package handlers

import (
	"net/url"
	"testing"
	"time"
)

func TestReportReachRequiresEventCoverageAndSource(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	form := url.Values{"reach_youtube_views": {"0"}}
	for _, key := range []string{"source", "period", "coverage", "date"} {
		if err := saveReportReach(form, map[string]string{}, now); err == nil {
			t.Fatalf("accepted metrics without %s", key)
		}
		value := "Nairobi export"
		if key == "date" {
			value = "2026-10-19"
		}
		form.Set("reach_youtube_"+key, value)
	}
	saved := map[string]string{}
	if err := saveReportReach(form, saved, now); err != nil {
		t.Fatal(err)
	}
	platforms := (sponsorReportPage{Saved: saved}).ReachPlatforms()
	if !platforms[0].Ready() || platforms[1].Ready() {
		t.Fatal("zero or missing platform mishandled")
	}
	if platforms[0].Metrics[0].Display() != "0" || platforms[0].Metrics[1].Display() != "" {
		t.Fatal("unknown converted to zero")
	}
	for _, invalid := range []string{"-1", "1.5", "1000000000001"} {
		form.Set("reach_youtube_views", invalid)
		if err := saveReportReach(form, map[string]string{}, now); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	form.Set("reach_youtube_views", "1234")
	form.Set("reach_youtube_date", "2026-10-20")
	if err := saveReportReach(form, map[string]string{}, now); err == nil {
		t.Fatal("accepted future export")
	}
}
