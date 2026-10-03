package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestReportCharts(t *testing.T) {
	views := int64(200)
	p := sponsorReportPage{Registrations: 320, Attendees: 286, Teams: 32, ProgramReviewed: true, Talks: 12, Workshops: 6, PanelCount: 0,
		Saved:   map[string]string{"analytics_metric": "YouTube views", "analytics_period": "Lifetime", "analytics_date": "2026-09-17", "analytics_source": "Studio export"},
		Popular: []reportSession{{Title: "Example talk", Views: &views}},
		Winners: []reportWinner{{Title: "No payout", Sats: 0}, {Title: "Paid project", Sats: 1000}}}
	charts := p.Charts()
	if len(charts) != 6 || charts[0].Rows[1].Width != 50 || charts[0].Rows[2].Width != 0 || charts[1].Rows[0].Width != 100 || charts[2].Rows[0].Label != "Paid project" {
		t.Fatalf("bad charts: %+v", charts)
	}
	if charts[3].Rows[1].Value != 286 || charts[3].Maximum != 320 || charts[4].Rows[0].Value != 32 || charts[4].Rows[1].Value != 2 {
		t.Fatal("attendance or project chart does not match database aggregates")
	}
	p.Winners = append(p.Winners, make([]reportWinner, 5)...)
	charts = p.Charts()
	if len(charts) != 7 || len(charts[3].Rows) != 2 || charts[3].Maximum != 1000 {
		t.Fatal("prize continuation loses projects or scale")
	}
	p.Preview = true
	for _, chart := range p.Charts() {
		if len(chart.Rows) != 0 {
			t.Fatal("preview leaks unverified numbers")
		}
	}
	p.Preview = false
	p.ProgramReviewed = false
	delete(p.Saved, "analytics_source")
	charts = p.Charts()
	if len(charts[0].Rows) != 0 || len(charts[1].Rows) != 0 {
		t.Fatal("unreviewed or unsourced chart rendered")
	}
}

func TestReportChartRendering(t *testing.T) {
	page := reportFixturePage(t)
	page.Preview = false
	page.ProgramReviewed = true
	page.Talks = 4
	page.Workshops = 2
	page.Winners = []reportWinner{{Title: "<script>unsafe</script>", Sats: 0}}
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if !strings.Contains(html, `class="donut-chart"`) || strings.Contains(html, "ZgotmplZ") || strings.Contains(html, "<script>unsafe</script>") {
		t.Fatal("invalid or unsafe chart rendering")
	}
	for _, removed := range []string{"Attendee-days", `name="day_`, "Attendance, day by day"} {
		if strings.Contains(html, removed) {
			t.Fatalf("unsupported daily attendance remains: %s", removed)
		}
	}
}

func TestReportDonutPartitions(t *testing.T) {
	c := reportChart{Kind: "teams", Rows: []reportBar{{Value: 32}, {Value: 3}}}
	slices := c.Donut()
	if len(slices) != 2 || slices[1].Value != 29 || c.DonutTotal() != 32 || slices[0].Percent+slices[1].Percent != 100 {
		t.Fatal("winning projects must be a subset of the total")
	}
	c.Rows[0].Value = 0
	if len(c.Donut()) != 0 {
		t.Fatal("invalid totals must not draw a donut")
	}
	c.Kind = "attendance"
	if len(c.Donut()) != 0 {
		t.Fatal("incomparable attendance units cannot form a whole")
	}
}
