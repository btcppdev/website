package handlers

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAnnualReportEventHighlights(t *testing.T) {
	p := annualFixture()
	pages := p.EventHighlightPages()
	if len(pages) != 2 || len(pages[0].Highlights) != 3 || len(pages[1].Highlights) != 3 {
		t.Fatal("expected three completed events per page")
	}
	if pages[0].Folio != "03 / 10" || p.ReportFolio(8) != "10 / 10" {
		t.Fatal("incorrect report numbering")
	}
	for _, page := range pages {
		for _, h := range page.Highlights {
			if h.Event.Tag == "seoul" {
				t.Fatal("future event mixed into recaps")
			}
			if h.Copy.Text == "" || h.Event.LeadImage == "" {
				t.Fatal("missing recap content")
			}
		}
	}
	if got := p.UpcomingHighlights(); len(got) != 1 || got[0].Status != "Upcoming" || !strings.Contains(got[0].Copy.Text, "upcoming") {
		t.Fatal("upcoming recap missing", got)
	}
	key := pages[0].Highlights[0].Copy.Key
	p.Saved[key] = "**A specific moment** worth sharing."
	if p.EventHighlightPages()[0].Highlights[0].Copy.Text != p.Saved[key] {
		t.Fatal("editor override lost")
	}
	p.Events[0].Included = false
	if len(p.EventHighlightPages()[1].Highlights) != 2 {
		t.Fatal("excluded event still featured")
	}
	p.Events = nil
	if len(p.EventHighlightPages()) != 0 || p.ReportFolio(8) != "08 / 08" {
		t.Fatal("empty roster creates extra pages")
	}
}
func TestAnnualReportEventHighlightForm(t *testing.T) {
	p := annualFixture()
	key := "event_highlight_" + p.Events[0].Event.ID
	f := url.Values{"action": {"save"}, "cutoff": {"2026-10-19"}, key: {"Our **edited** recap."}}
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	if err := annualReportForm(&p, f, now); err != nil {
		t.Fatal(err)
	}
	if p.Saved[key] != "Our **edited** recap." {
		t.Fatal("recap not saved")
	}
	f.Del(key)
	if err := annualReportForm(&p, f, now); err != nil || p.Saved[key] == "" {
		t.Fatal("older form discarded recap", err)
	}
	f.Set(key, strings.Repeat("x", 2001))
	if annualReportForm(&p, f, now) == nil {
		t.Fatal("oversized recap accepted")
	}
	f.Del(key)
	f.Set("event_highlight_invalid", "text")
	if annualReportForm(&p, f, now) == nil {
		t.Fatal("invalid event identifier accepted")
	}
}

func TestAnnualReportEventStats(t *testing.T) {
	e := annualEvent{Talks: 24, Workshops: 5, Panels: 3, HackCoverage: "records", Sats: 2500000,
		Media: []reportMedia{{Platform: "X", ID: "one", Views: 1000}, {Platform: "X", ID: "one", Views: 1000}, {Platform: "YouTube", ID: "two", Views: 2000}}}
	stats := annualEventStats(e)
	if stats[0].Value != "24" || stats[1].Value != compactReportSats(3000) || stats[2].Value != compactReportSats(2500000) {
		t.Fatalf("incorrect counters: %+v", stats)
	}
	e.Media = nil
	e.HackCoverage = "unknown"
	stats = annualEventStats(e)
	if stats[1].Value != "—" || stats[2].Value != "—" {
		t.Fatal("unknown data presented as a number")
	}
	e.HackCoverage = "none"
	if annualEventStats(e)[2].Label != "no hackathon" {
		t.Fatal("missing no-hackathon label")
	}
	p := annualFixture()
	if len(p.UpcomingHighlights()[0].Stats) != 0 {
		t.Fatal("upcoming event has completed-event stats")
	}
}
