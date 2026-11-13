package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestAnnualReportReviewIncludesEventRecaps(t *testing.T) {
	p := annualFixture()
	for i := range p.Events {
		if !p.Events[i].HackKnown() {
			continue
		}
		p.Events[i].WinningProjects = []reportWinner{{ID: p.Events[i].Event.Tag + "-a", Title: "Project A"}, {ID: p.Events[i].Event.Tag + "-b", Title: "Project B"}}
	}
	highlights := p.BriefProjects()
	if len(highlights) != 3 {
		t.Fatal("expected three project highlights")
	}
	if highlights[0].Tag == highlights[1].Tag {
		t.Fatal("selection should span events before repeating one")
	}
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl", "../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	main := out.String()[strings.Index(out.String(), "<main>"):]
	if got := strings.Count(main, `<section class="paper-sheet`); got != 10 {
		t.Fatalf("got %d report sections", got)
	}
	if !strings.Contains(main, `aria-label="Annual report cover"`) || strings.Index(main, "impact-cover-recipient") > strings.Index(main, `data-section="note"`) {
		t.Fatal("dedicated cover must precede the letter")
	}
	for _, bad := range []string{"[Add ", "[Describe ", "[Highlight ", "Program by event", "Sources &amp; definitions", "Project notebook"} {
		if strings.Contains(main, bad) {
			t.Fatalf("long-report detail or prompt leaked: %s", bad)
		}
	}
}

func TestAnnualReportBriefPreservesEditorialCopy(t *testing.T) {
	p := annualFixture()
	original := p.Section("program").Text
	if p.BriefSection("program").Text != original {
		t.Fatal("real program prose changed")
	}
	p.Sections = append(p.Sections, reportSection{Key: "test", Text: "Keep this paragraph.\n\n[Add a quote.]\n\nAnd this paragraph."})
	if got := p.BriefSection("test").Text; got != "Keep this paragraph.\n\nAnd this paragraph." {
		t.Fatal(got)
	}
	if !strings.Contains(p.Section("test").Text, "[Add a quote.]") {
		t.Fatal("editor source was changed")
	}
}

func TestAnnualReportHackathonParticipationCountsEachEvent(t *testing.T) {
	// Two people join the first event, and one returns for the second.
	// The headline should count three participations, not two unique people.
	p := annualReportPage{UniqueParticipants: 2, Events: []annualEvent{
		{Included: true, HackCoverage: "records", Participants: 2},
		{Included: true, HackCoverage: "automatic", Participants: 1},
		{Included: false, HackCoverage: "records", Participants: 10},
		{Included: true, HackCoverage: "incomplete", Participants: 20},
	}}
	metrics := p.BuildingMetrics()
	if len(metrics) != 1 || metrics[0].Label != "hackathon participations" || metrics[0].Value != "3" {
		t.Fatalf("participation headline = %+v, want 3 hackathon participations", metrics)
	}
	p.Events[0].Included = false
	p.Events[1].Included = false
	if metrics = p.BuildingMetrics(); len(metrics) != 0 {
		t.Fatalf("excluded or incomplete events contributed to headline: %+v", metrics)
	}
}
