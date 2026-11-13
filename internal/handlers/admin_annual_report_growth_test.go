package handlers

import (
	"bytes"
	"html/template"
	"math"
	"strings"
	"testing"
	"time"
)

func annualGrowthFixture() annualReportPage {
	first := time.Date(2026, time.March, 2, 1, 0, 0, 0, time.UTC)
	second := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	return annualReportPage{Year: 2026, Cutoff: "2026-11-12", Recipient: "NYDIG", RegistrationBased: true, OfferedBased: true, UniqueParticipants: 2, Events: []annualEvent{
		{Included: true, HackCoverage: "automatic", Event: reportEvent{Location: "Second", End: &second, Timezone: "UTC"}, Registrations: 200, Attendees: 20, Projects: 7, Participants: 1, Sats: 2000000},
		{Included: true, HackCoverage: "records", Event: reportEvent{Location: "First", End: &first, Timezone: "America/New_York"}, Registrations: 100, Attendees: 10, Projects: 3, Participants: 2, Sats: 1000000},
		{Included: false, HackCoverage: "records", Registrations: 999, Projects: 999, Participants: 999, Sats: 999},
		{Included: true, HackCoverage: "incomplete", Projects: 999, Participants: 999, Sats: 999},
	}}
}

func TestAnnualReportGrowthUsesCompletedEventTotals(t *testing.T) {
	p := annualGrowthFixture()
	c := p.RegistrationGrowth()
	if c == nil || c.Total != p.ParticipationTotal() || len(c.Points) != 2 {
		t.Fatalf("registration growth = %+v", c)
	}
	if c.Points[0].Event != "First" || c.Points[0].Date != "2026-03-01" || c.Points[0].Value != 100 || c.Points[1].Value != 300 {
		t.Fatalf("not chronological local end dates: %+v", c.Points)
	}
	// Use elapsed calendar time, not equally spaced event positions, and keep
	// zero visible even though the first observation is already positive.
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end, _ := time.Parse(time.DateOnly, p.Cutoff)
	wantX := 40 + 400*first.Sub(start).Hours()/end.Sub(start).Hours()
	if math.Abs(c.Points[0].X-wantX) > .001 || c.Points[0].Y != 116 || c.Points[1].Y != 20 || !strings.HasPrefix(c.Line(), "40,164 ") || !strings.HasSuffix(c.Line(), "440,20.00") {
		t.Fatalf("incorrect axes: %+v, line %s", c.Points, c.Line())
	}
	prizes := p.PrizeGrowth()
	if prizes == nil || prizes.Total != 3000000 || prizes.Label != "in prizes" || prizes.Unit != "sats" {
		t.Fatalf("prize chart must use selected known offers: %+v", prizes)
	}
	building := p.BuildingGrowth()
	if len(building) != 2 || building[0].Total != 10 || building[1].Total != 3 || building[1].Label != "hackathon participations" {
		t.Fatalf("building series must count participation at each event: %+v", building)
	}
	p.RegistrationBased = false
	p.OfferedBased = false
	if c = p.RegistrationGrowth(); c.Total != 30 || c.Label != "event attendances" {
		t.Fatalf("legacy attendance basis changed: %+v", c)
	}
	if c = p.PrizeGrowth(); c.Label != "sats sent / claimed" {
		t.Fatalf("legacy payouts relabeled: %+v", c)
	}
}

func TestAnnualReportGrowthGroupsSameDayAndPreservesHeadlines(t *testing.T) {
	p := annualGrowthFixture()
	e := p.Events[1]
	e.Event.Location = "Also first"
	p.Events = append(p.Events, e)
	c := p.RegistrationGrowth()
	if c == nil || len(c.Points) != 2 || c.Points[0].Value != 200 || c.Total != 400 || !strings.Contains(c.Points[0].Event, "Also first") {
		t.Fatalf("simultaneous events not combined: %+v", c)
	}
	// A single dated participant total stays visible alongside the project chart.
	p.Events[0].Participants = 0
	building := p.BuildingGrowth()
	if len(building) != 2 || len(building[0].Points) != 2 || building[1].Total != 4 || len(building[1].Points) != 0 {
		t.Fatalf("missing series removed a known metric: %+v", building)
	}
	p.Events[0].Event.End = nil
	if p.RegistrationGrowth() != nil || p.PrizeGrowth() != nil {
		t.Fatal("missing dates must fall back to totals, not chart a partial sum")
	}
	for i := range p.Events {
		p.Events[i].Included = false
	}
	if p.RegistrationGrowth() != nil || p.PrizeGrowth() != nil || len(p.BuildingGrowth()) != 0 {
		t.Fatal("excluded observations appeared in charts")
	}
}

func TestAnnualReportGrowthRejectsOutOfScopeDates(t *testing.T) {
	for _, cutoff := range []string{"2026-01-01", "2026-06-01", "2027-01-01"} {
		p := annualGrowthFixture()
		p.Cutoff = cutoff
		if p.RegistrationGrowth() != nil {
			t.Fatalf("chart extended outside its scope: %s", cutoff)
		}
	}
}

func TestAnnualReportGrowthRender(t *testing.T) {
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl", "../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	p := annualFixture()
	var out bytes.Buffer
	if err = tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), `class="annual-growth annual-growth--`) != 4 || strings.Count(out.String(), `class="annual-growth-line"`) != 4 || strings.Contains(out.String(), "ZgotmplZ") {
		t.Fatal("expected four valid growth charts")
	}
}
