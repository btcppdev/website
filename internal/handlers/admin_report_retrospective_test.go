package handlers

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReportHackathonRetrospective(t *testing.T) {
	p := sponsorReportPage{Event: reportEvent{Tag: "floripa26"}, Saved: map[string]string{}}
	p.Sections = eventReportSections(p.Event)
	p.applyHackathonRetrospective()
	p.applyHackathonRetrospective()
	hydrateEventReport(&p)
	if p.Teams != 21 || p.Participants != 60 || p.ParticipantsDisplay() != "~60" || p.Sats != 0 || len(p.Winners) != 2 {
		t.Fatalf("incorrect sourced totals: %+v", p)
	}
	if p.HackathonRetrospective.Bugs != 10 || p.HackathonRetrospective.Hours != 22 || len(p.TeamSizes) != 0 {
		t.Fatal("invented team sizes or missing published results")
	}
	if p.Winners[0].Title != "MineExploit" || p.Winners[0].PlaceRank() != 1 || p.Winners[1].Title != "LocalProbe" || p.Winners[1].PlaceRank() != 2 {
		t.Fatal("published placements changed")
	}
	for _, winner := range p.Winners {
		if _, err := uuid.Parse(winner.ID); err != nil {
			t.Fatal("editorial project key must be a stable UUID", err)
		}
		if winner.ReportURL(p.Event.Tag) != p.HackathonURL() || winner.PrizeFigure() != nil || len(winner.Portraits) != 0 {
			t.Fatal("unpublished project identity or prize data was invented")
		}
	}
	for _, chart := range p.Charts() {
		if chart.Kind == "teams" || chart.Kind == "team-sizes" || chart.Kind == "prizes" {
			t.Fatal("partial winner roster was presented as a complete distribution")
		}
	}
	for _, metric := range p.Metrics {
		if metric.Label == "Sats awarded" || metric.Label == "Sponsor challenges" {
			t.Fatal("unknown source metrics displayed as zero")
		}
	}
	for _, tag := range []string{"floripa", "nairobi", "berlin26"} {
		other := sponsorReportPage{Event: reportEvent{Tag: tag}}
		other.applyHackathonRetrospective()
		if other.Teams != 0 || other.HackathonRetrospective != nil {
			t.Fatal("retrospective leaked into another event", tag)
		}
	}
	existing := sponsorReportPage{Event: p.Event, Teams: 5, Participants: 10}
	existing.applyHackathonRetrospective()
	if existing.Teams != 5 || existing.HackathonRetrospective != nil {
		t.Fatal("standalone fallback overwrote connected project records")
	}

	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "~60") || !strings.Contains(out.String(), p.HackathonURL()) || strings.Contains(out.String(), "/floripa26/hackathon") {
		t.Fatal("event report lost the approximate count or linked to the missing hackathon")
	}
}

func retrospectiveAnnualFixture() annualReportPage {
	p := annualReportPage{Year: 2026, Recipient: "NYDIG", Cutoff: "2026-11-12", Generated: "2026-11-13T00:00:00Z", Saved: map[string]string{}, OfferedBased: true}
	for i, tag := range []string{"earlier", "floripa26", "later"} {
		end := time.Date(2026, time.Month(i+1), 28, 12, 0, 0, 0, time.UTC)
		e := annualEvent{Event: reportEvent{ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte(tag)).String(), Tag: tag, Location: tag, Start: &end, End: &end, Timezone: "UTC"}, Included: true, Eligible: true, HackCoverage: "automatic", Projects: 7, Participants: int64(10 * (i + 1)), Sats: 1000000, OfferedBased: true}
		if tag == "floripa26" {
			e.HackCoverage, e.Sats = "incomplete", 0
			e.Event.Location = "Florianópolis"
			e.Event.LeadImage = "/static/img/floripa26/leading.png"
			e.applyHackathonRetrospective(p.Year)
		}
		p.Events = append(p.Events, e)
	}
	p.Sections = annualSections(p.Year, p.Recipient)
	return p
}

func TestAnnualReportHackathonRetrospective(t *testing.T) {
	p := retrospectiveAnnualFixture()
	e := p.Events[1]
	if !e.HackKnown() || e.HackCoverage != "retrospective" || e.Projects != 21 || !e.ParticipantsEstimated || e.Winners != 0 {
		t.Fatal("historical coverage was not replaced with the scoped source")
	}
	if e.Sats != 8_000_000 || !e.PrizesEstimated || e.SatsDisplay() != "~8M" {
		t.Fatal("organizer prize estimate was omitted or presented as exact")
	}
	metrics := p.BriefHackMetrics()
	if metrics[0].Value != "35" || metrics[1].Value != "~100" || metrics[2].Value != "~10M" {
		t.Fatalf("source totals were doubled or lost their estimate: %+v", metrics)
	}
	charts := p.BuildingGrowth()
	if charts[0].Display() != "35" || charts[1].Display() != "~100" || charts[1].Points[0].Estimated || !charts[1].Points[1].Estimated || charts[1].Points[2].Display() != "~100" {
		t.Fatal("cumulative chart lost the estimate or changed exact project counts")
	}
	prizes := p.PrizeGrowth()
	if prizes == nil || prizes.Display() != "~10M" || prizes.Points[0].Estimated || prizes.Points[1].Display() != "~9000000" || prizes.Points[2].Display() != "~10000000" {
		t.Fatal("cumulative prize chart lost the organizer estimate")
	}
	projects := p.BriefProjects()
	if len(projects) != 1 || projects[0].Project.Title != "MineExploit" || projects[0].URL() != e.HackathonURL() {
		t.Fatal("first-place gallery did not use the published winner and source")
	}
	p.Events[1].Included = false
	if p.BriefHackMetrics()[1].Value != "40" || p.PrizeTotalDisplay() != "2M" || len(p.BriefProjects()) != 0 {
		t.Fatal("excluded event remained in totals or winner gallery")
	}
	for _, coverage := range []string{"none", "excluded", "records"} {
		x := annualEvent{Event: e.Event, HackCoverage: coverage, Projects: 5}
		x.applyHackathonRetrospective(2026)
		if x.Projects != 5 || x.HackathonRetrospective != nil {
			t.Fatal("explicit coverage choice was ignored", coverage)
		}
	}
	x := annualEvent{Event: e.Event, HackCoverage: "incomplete"}
	x.applyHackathonRetrospective(2025)
	if x.HackKnown() || x.Projects != 0 {
		t.Fatal("source leaked into another report year")
	}
	for _, offered := range []bool{false, true} {
		x := annualEvent{Event: e.Event, HackCoverage: "retrospective", OfferedBased: offered, Sats: 5_000_000}
		x.applyHackathonRetrospective(2026)
		if x.Sats != 5_000_000 || x.PrizesEstimated {
			t.Fatal("organizer estimate replaced known prize records")
		}
	}
	x = annualEvent{Event: e.Event, HackCoverage: "retrospective", AwardBased: true}
	x.applyHackathonRetrospective(2026)
	if x.Sats != 0 || x.PrizesEstimated {
		t.Fatal("prize offer estimate was treated as an award")
	}
}

func TestAnnualReportRetrospectiveRenderAndSnapshot(t *testing.T) {
	p := retrospectiveAnnualFixture()
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl", "../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"MineExploit", "~100", "~60 participants", "~8M", "~10M", "22 hours", "bugs found", "Read retrospective", p.Events[1].HackathonURL()} {
		if !strings.Contains(body, want) {
			t.Error("missing", want)
		}
	}
	if strings.Contains(body, "/floripa26/hackathon") {
		t.Fatal("missing hackathon page linked")
	}
	form := url.Values{"action": {"save"}, "cutoff": {p.Cutoff}, "hack_" + p.Events[1].Event.ID: {"retrospective"}, "project_built_" + p.BriefProjects()[0].Project.ID: {"Reviewed winner summary"}}
	if err := annualReportForm(&p, form, time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	p.Frozen = p.Generated
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var frozen annualReportPage
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.Events[1].HackathonRetrospective.URL != p.Events[1].HackathonURL() || !frozen.Events[1].ParticipantsEstimated || !frozen.Events[1].PrizesEstimated || frozen.PrizeTotalDisplay() != "~10M" || frozen.BriefProjects()[0].Story.Built != "Reviewed winner summary" {
		t.Fatal("frozen edition lost its source, estimate or editorial copy")
	}
	// With only one event there is no cumulative chart; the fallback must still
	// label the estimated prize total, including in its accessible description.
	p.Events = p.Events[1:2]
	out.Reset()
	if err = tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	if p.PrizeGrowth() != nil || !strings.Contains(out.String(), `aria-label="~8M sats offered in prizes; event breakdown below"`) || !strings.Contains(out.String(), `class="funding-total">~8M</text>`) {
		t.Fatal("single-event prize fallback lost its estimate marker")
	}
}
