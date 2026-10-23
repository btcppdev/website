package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestReportMediaToplineDeduplicatesLatest(t *testing.T) {
	media := []reportMedia{
		{Platform: "X", ID: "one", Views: 950, Collected: "2026-10-21T12:00:00Z"},
		{Platform: "X", ID: "one", Views: 900, Collected: "2026-10-22T12:00:00Z"},
		{Platform: "X", ID: "two", Views: 100, Collected: "2026-10-22T12:00:00Z"},
		{Platform: "YouTube", ID: "one", Views: 999, Collected: "2026-10-22T12:00:00Z"},
	}
	totals := reportMediaTotals(media)
	if len(totals) != 1 || totals[0].Platform != "X" || totals[0].Views != 1000 || totals[0].Recordings != 2 {
		t.Fatalf("wrong topline: %+v", totals)
	}
	if media[0].Views != 950 {
		t.Fatal("source data mutated")
	}
	annual := annualReportPage{Events: []annualEvent{{Included: true, Media: media[:2]}, {Included: true, Media: media[1:]}, {Included: false, Media: []reportMedia{{Platform: "X", ID: "excluded", Views: 5000}}}}}
	if got := annual.MediaTotals(); len(got) != 1 || got[0].Views != 1000 {
		t.Fatalf("annual scope/deduplication: %+v", got)
	}
}

func TestReportToplineThresholds(t *testing.T) {
	if (sponsorReportPage{Registrations: 99}).ShowRegistrations() {
		t.Fatal("low registration headline")
	}
	if !(sponsorReportPage{Registrations: 100}).ShowRegistrations() {
		t.Fatal("100 should qualify")
	}
	if (sponsorReportPage{Registrations: 200, Preview: true}).ShowRegistrations() {
		t.Fatal("preview registrations presented as connected")
	}
	for _, key := range []string{"reach_x_views", "reach_youtube_external_views", "reach_x_impressions"} {
		if (reportReachMetric{Key: key, Value: "999"}).Headline() {
			t.Fatal("low reach headline", key)
		}
		if !(reportReachMetric{Key: key, Value: "1000"}).Headline() {
			t.Fatal("threshold rejected", key)
		}
	}
	p := annualReportPage{RegistrationBased: true, Events: []annualEvent{{Included: true, Registrations: 80}, {Included: true, Registrations: 60}}}
	if p.ParticipationTotal() != 140 || !p.ShowParticipation() {
		t.Fatal("small events excluded from aggregate")
	}
}

func TestReportToplineTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl", "../../templates/admin/annual_report.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	totals := reportMediaTotals([]reportMedia{{Platform: "YouTube", ID: "a", Views: 600}, {Platform: "YouTube", ID: "b", Views: 500}})
	if err := tmpl.ExecuteTemplate(&out, "report_media_totals", totals); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1.1k", "Across 2 matched recordings", "Lifetime views"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing", want)
		}
	}
	p := annualFixture()
	p.Events[0].Registrations = 73
	p.Events[0].Media = []reportMedia{{Platform: "YouTube", ID: "a", Views: 600}, {Platform: "YouTube", ID: "b", Views: 500}}
	out.Reset()
	if err := tmpl.ExecuteTemplate(&out, "annual_report.tmpl", p); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	main := html[strings.Index(html, "<main>"):]
	if strings.Contains(main, ">73<") || strings.Contains(main, ">600<") || strings.Contains(main, ">500<") {
		t.Fatal("small attendance/video counters leaked into report")
	}
	if !strings.Contains(main, "1.1k") {
		t.Fatal("annual aggregate missing")
	}
	if !strings.Contains(html, "600 views") {
		t.Fatal("source counts missing from editor")
	}
}
