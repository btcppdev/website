package handlers

import (
	"bytes"
	"html"
	"html/template"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAnnualReportComparableGatherings(t *testing.T) {
	event := func(date, zone string) reportEvent {
		d, err := time.Parse(time.RFC3339, date)
		if err != nil {
			t.Fatal(err)
		}
		return reportEvent{Start: &d, End: &d, Timezone: zone}
	}
	p := annualReportPage{Year: 2026, Cutoff: "2026-11-12", Events: []annualEvent{{Included: true, Eligible: true}, {Included: true, Eligible: true}}}
	previous := []reportEvent{event("2025-06-01T12:00:00Z", "UTC"), event("2025-11-13T01:00:00Z", "America/Los_Angeles"), event("2025-11-13T01:00:00Z", "UTC"), event("2024-06-01T12:00:00Z", "UTC"), {}}
	c := annualGatheringComparison(p, previous)
	if c == nil || c.Previous != 2 || c.Current != 2 || c.Through != "12 November" || c.PreviousWidth() != 100 {
		t.Fatalf("comparison did not respect matching local cutoffs: %+v", c)
	}
	p.Events[0].Included = false
	if annualGatheringComparison(p, previous) != nil {
		t.Fatal("selection exclusions created an unequal comparison")
	}
	p.Events[0].Included = true
	if annualGatheringComparison(p, nil) != nil {
		t.Fatal("missing historical coverage treated as zero")
	}
	p.Year, p.Cutoff = 2024, "2024-02-29"
	c = annualGatheringComparison(p, []reportEvent{event("2023-02-28T12:00:00Z", "UTC"), event("2023-03-01T00:00:00Z", "UTC")})
	if c == nil || c.Previous != 1 {
		t.Fatalf("leap-day comparison included March: %+v", c)
	}
}

func TestAnnualReportImpactCopyPreservesEdits(t *testing.T) {
	p := annualFixture()
	for _, old := range legacyAnnualSections(p.Year, p.Recipient) {
		p.Saved[old.Key] = old.Text
	}
	p.Saved["community"] = "Our custom participant story."
	p.Saved["reach"] = ""
	p.loadNarrativeSections()
	if !strings.Contains(p.Section("note").Text, "2027") || p.Section("community").Text != "Our custom participant story." || p.Section("reach").Text != "" {
		t.Fatal("default upgrade lost custom copy or intentional blanks")
	}
	for _, key := range []string{"resource_use", "lessons"} {
		if p.BriefSection(key).Text != "" {
			t.Fatalf("unfinished %s copy leaked into report", key)
		}
	}
	if !strings.Contains(p.BriefSection("support").Text, "crew travel, catering, event space, and professional video recordings") {
		t.Fatal("organizer-confirmed NYDIG contribution missing")
	}
	if !strings.Contains(p.BriefSection("community_quote").Text, "You do a great job to make all of us meet.") {
		t.Fatal("organizer-supplied community feedback missing")
	}
	hrf := annualReportPage{Year: 2026, Recipient: "HRF", Sections: annualSections(2026, "HRF")}
	if hrf.BriefSection("support").Text != "" || strings.Contains(hrf.Section("note").Text, "crew travel") {
		t.Fatal("NYDIG-specific allocation applied to another recipient")
	}
	p.RepeatRegistrants = 12
	if m := p.RepeatParticipation(); m == nil || m.Value != "12" || !strings.Contains(m.Label, "registered") {
		t.Fatalf("wrong repeat participation: %+v", m)
	}
	p.RegistrationBased = false
	p.ReturningAttendees, p.UniqueAttendees = 3, 10
	if m := p.RepeatParticipation(); m == nil || !strings.Contains(m.Label, "checked in") {
		t.Fatalf("legacy check-ins relabeled: %+v", m)
	}
}

func TestAnnualReportProjectFollowupEvidence(t *testing.T) {
	w := reportWinner{ID: "43a0dcc8-c4eb-45fc-abd7-6546a86d1037", Description: "Original description"}
	p := annualReportPage{Year: 2026, Cutoff: "2026-10-16", Saved: map[string]string{}}
	if p.projectStory(w).Next != "" {
		t.Fatal("future follow-up included before cutoff")
	}
	p.Cutoff = "2026-10-17"
	story := p.projectStory(w)
	if !strings.Contains(story.Next, "conclusion") || !strings.Contains(story.Source, "/commit/") {
		t.Fatalf("verified follow-up missing: %+v", story)
	}
	p.Saved["project_next_"+w.ID] = ""
	if p.projectStory(w).Next != "" {
		t.Fatal("cleared follow-up restored itself")
	}
	p.Frozen = "2026-10-17"
	if got := p.projectStory(w); got.Problem != "" || got.Built != "Original description" {
		t.Fatalf("old frozen project changed: %+v", got)
	}
	p.Saved["project_problem_"+w.ID] = "Saved challenge"
	p.Saved["project_built_"+w.ID] = "Saved prototype"
	if got := p.projectStory(w); got.Problem != "Saved challenge" || got.Built != "Saved prototype" {
		t.Fatalf("frozen story not preserved: %+v", got)
	}
}

func TestAnnualReportProjectStoryForm(t *testing.T) {
	p := annualFixture()
	id := "43a0dcc8-c4eb-45fc-abd7-6546a86d1037"
	key := "project_next_" + id
	source := "project_source_" + id
	f := url.Values{"action": {"save"}, "cutoff": {"2026-10-19"}, key: {"Documented follow-up."}, source: {"https://example.test/release"}, "support": {"Confirmed contribution."}}
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	if err := annualReportForm(&p, f, now); err != nil {
		t.Fatal(err)
	}
	f.Del(key)
	f.Del(source)
	f.Del("support")
	if err := annualReportForm(&p, f, now); err != nil || p.Saved[key] != "Documented follow-up." || p.Saved["support"] != "Confirmed contribution." {
		t.Fatal("older form lost editorial evidence", err)
	}
	f.Set(source, "javascript:alert(1)")
	if annualReportForm(&p, f, now) == nil {
		t.Fatal("unsafe source URL accepted")
	}
	f.Del(source)
	f.Set(key, strings.Repeat("x", 1201))
	if annualReportForm(&p, f, now) == nil {
		t.Fatal("oversized story accepted")
	}
	f.Del(key)
	f.Set("project_built_invalid", "text")
	if annualReportForm(&p, f, now) == nil {
		t.Fatal("invalid project ID accepted")
	}
}

func TestAnnualReportImpactRender(t *testing.T) {
	p := annualFixture()
	p.GatheringComparison = &annualYearComparison{PreviousYear: 2025, Year: 2026, Previous: 5, Current: 6, Through: "19 October"}
	p.PartnerEvents = []annualPartnerEvent{{Event: p.Events[0].Event, Headline: true}}
	p.Events[3].WinningProjects = []reportWinner{{ID: "43a0dcc8-c4eb-45fc-abd7-6546a86d1037", Title: "template-watcher", Rank: 1}}
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl", "../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	main := html.UnescapeString(out.String()[strings.Index(out.String(), "<main>"):])
	for _, want := range []string{"Gatherings, year over year.", "people registered for 2+ gatherings", "You do a great job to make all of us meet.", "A dashboard comparing mining pools", "Bring builders together."} {
		if !strings.Contains(main, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{"[Add ", "How support was used", "A participant’s voice", "ZgotmplZ", `class="paper-source"`, "Cumulative at event close", "After the event", "Source ↗"} {
		if strings.Contains(main, bad) {
			t.Errorf("unfilled evidence or invalid output: %s", bad)
		}
	}
}
