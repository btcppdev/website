package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"
)

func TestAnnualReportUpcomingShowcase(t *testing.T) {
	p := annualFixture()
	total := p.ParticipationTotal()
	program := p.ProgramChart().DonutTotal()
	if got := p.Forthcoming(); len(got) != 1 || got[0].Event.Tag != "seoul" || got[0].Status != "Upcoming" {
		t.Fatalf("missing Seoul: %+v", got)
	}
	if len(p.CoverEvents()) != 7 {
		t.Fatal("cover omitted an event")
	}
	p.Events[len(p.Events)-1].Registrations = 9000
	p.Events[len(p.Events)-1].Talks = 9000
	p.Events[len(p.Events)-1].Sats = 900000000
	if p.ParticipationTotal() != total || p.ProgramChart().DonutTotal() != program || p.FundingChart().DonutTotal() != 15000000 {
		t.Fatal("upcoming event inflated completed totals")
	}
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl", "../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/static/img/seoul/leading.png", "Upcoming", "Still ahead in 2026", "https://btcpp.dev/seoul"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing showcase", want)
		}
	}
	// A past event beyond an old cutoff must not be mislabeled upcoming.
	p.Generated = "2026-12-01T00:00:00Z"
	if len(p.Forthcoming()) != 0 {
		t.Fatal("past event labeled upcoming")
	}
	// An event in the next calendar year is not part of this year's cover.
	next := time.Date(2027, time.January, 5, 0, 0, 0, 0, time.UTC)
	p.Events[len(p.Events)-1].Event.Start = &next
	if len(p.Forthcoming()) != 0 {
		t.Fatal("next year's event included")
	}
}
