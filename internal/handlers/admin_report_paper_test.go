package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestReportPaperUsesPublicEvidenceWithoutInventedMetrics(t *testing.T) {
	p := reportFixturePage(t)
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "report_paper", p); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, value := range []string{"36", "5M", "WestToEast", "Robin", "nairobi2", "sats awarded", "paper-chart", "Sources & reading this report"} {
		if !strings.Contains(html, value) {
			t.Errorf("missing %q", value)
		}
	}
	for _, forbidden := range []string{"Data pending", "286", "840,000", "24 talks", "SYNTHETIC", "Awaiting"} {
		if strings.Contains(html, forbidden) {
			t.Errorf("public report contains %q", forbidden)
		}
	}
	if got := strings.Count(html, `class="paper-sheet`); got != 11 {
		t.Fatalf("paper sections=%d, want 11", got)
	}
	chart := p.PublishedPrizeChart()
	if chart == nil || chart.DonutTotal() != 5000000 {
		t.Fatal("incorrect published overall award total")
	}
	if p.Sats != 0 || p.Attendees != 0 || p.Teams != 0 {
		t.Fatal("public evidence contaminated database metrics")
	}
	p.Preview = false
	if p.PublishedPrizeChart() != nil {
		t.Fatal("public snapshot mixed into live database report")
	}
}

func TestReportPaperOmitsUnavailableCharts(t *testing.T) {
	p := reportFixturePage(t)
	p.PublicVideoSource = ""
	if len(p.ReadyCharts()) != 0 {
		t.Fatal("public fixture must not imply private metrics")
	}
	if p.ChartFor("views") != nil {
		t.Fatal("unsourced recording analytics shown")
	}
}

func TestReportHeadlineCounts(t *testing.T) {
	p := reportFixturePage(t)
	facts := p.HeadlineEvidence()
	if len(facts) != 4 || facts[0].Value != "44" || facts[1].Value != "9" || facts[1].Label != "scheduled workshops" || facts[2].Value != "36" || facts[3].Value != "—" {
		t.Fatal("public summary must distinguish scheduled workshops and unknown participants")
	}
	p.Preview, p.ProgramReviewed = false, true
	p.Workshops, p.Teams, p.Participants = 8, 30, 74
	facts = p.HeadlineEvidence()
	if facts[1].Label != "workshops held" || facts[1].Value != "8" || facts[2].Value != "30" || facts[3].Value != "74" {
		t.Fatal("summary did not use reviewed delivery and live hackathon counts")
	}
}
