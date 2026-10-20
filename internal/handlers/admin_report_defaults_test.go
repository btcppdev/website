package handlers

import "testing"

func TestReportProductionDefaultsKeepLiveMetrics(t *testing.T) {
	p := sponsorReportPage{Saved: map[string]string{}, Attendees: 123, Participants: 45, Sessions: []reportSession{{ID: "live-session", YouTube: "https://www.youtube.com/watch?v=Mn18G9EZPMQ"}, {ID: "unmatched", YouTube: "https://youtu.be/not-in-snapshot"}}}
	if err := applyNairobiReportDefaults(&p); err != nil {
		t.Fatal(err)
	}
	if p.Attendees != 123 || p.Participants != 45 || p.Preview || p.DemoAttendance || p.Synthetic || p.ProgramReviewed {
		t.Fatal("defaults replaced production metrics or enabled demo mode")
	}
	if p.PreparedFor() != "Btrust" || p.Saved["views_live-session"] != "48" || p.Saved["views_unmatched"] != "" {
		t.Fatal("incorrect recipient or recording match")
	}
	if p.Saved["reach_youtube_views"] != "2811" || p.Saved["analytics_date"] != "2026-10-19" {
		t.Fatal("dated video snapshot missing")
	}
}
