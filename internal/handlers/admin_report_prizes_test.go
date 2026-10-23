package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestReportWinningProjectsOnly(t *testing.T) {
	p := reportFixturePage(t)
	p.Projects = append(p.Projects, reportWinner{ID: "not-a-winner", Title: "Unawarded project must not be featured"})
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = tmpl.ExecuteTemplate(&out, "report_paper", p); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, bad := range []string{"project-wall-", "Unawarded project must not be featured", "/projects/featured-"} {
		if strings.Contains(body, bad) {
			t.Fatal("unawarded gallery remains", bad)
		}
	}
	for _, amount := range []string{"2.5M", "1.5M", "1M", "sats awarded"} {
		if !strings.Contains(body, amount) {
			t.Fatal("missing prize", amount)
		}
	}
	for _, w := range p.Winners {
		if !strings.Contains(body, w.Title) {
			t.Fatal("winner omitted", w.Title)
		}
	}
}
func TestReportPrizeFigureBasis(t *testing.T) {
	w := reportWinner{AwardedSats: 1500000, Sats: 500000}
	if f := w.PrizeFigure(); f.Value != "1.5M" || f.Label != "sats awarded" {
		t.Fatal(f)
	}
	w.AwardedSats = 0
	if f := w.PrizeFigure(); f.Value != "500k" || f.Label != "sats sent / claimed" {
		t.Fatal(f)
	}
	if (reportWinner{}).PrizeFigure() != nil {
		t.Fatal("missing prize shown as zero")
	}
}

func TestReportWinnerTeamMixedPhotos(t *testing.T) {
	w := reportWinner{Portraits: []reportPerson{{Name: "Ada Builder", Photo: "https://example.test/ada.jpg"}, {Name: "Bo Maker"}}}
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = tmpl.ExecuteTemplate(&out, "report_winner_team", w); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"ada.jpg", "Ada Builder", "Bo Maker", ">BM</span>"} {
		if !strings.Contains(body, want) {
			t.Fatal("missing team information", want)
		}
	}
	if strings.Count(body, "<img") != 1 {
		t.Fatal("missing photo replaced with an image")
	}
}

func TestReportPodiumOrder(t *testing.T) {
	p := sponsorReportPage{Winners: []reportWinner{{Title: "Third", Rank: 3}, {Title: "Sponsor first prize", Rank: -1, Awards: "First prize"}, {Title: "Second", Rank: 2}, {Title: "First", Rank: 1}, {Title: "Mention", Rank: 4}}}
	podium := p.PodiumWinners()
	if len(podium) != 3 || podium[0].Title != "First" || podium[1].Title != "Second" || podium[2].Title != "Third" {
		t.Fatal("incorrect podium", podium)
	}
	if len(p.OtherWinners()) != 2 {
		t.Fatal("other winners lost")
	}
}
