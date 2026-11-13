package handlers

import (
	"fmt"
	"reflect"
	"testing"
)

// A dense, explicitly synthetic edition for browser/PDF regression checks.
// Public portraits and copy are retained; all added records are test data.
func reportPrintFixture(t *testing.T) sponsorReportPage {
	p := reportFixturePage(t)
	p.Preview, p.Synthetic, p.ProgramReviewed = false, true, true
	p.Message = "Synthetic print test: counts, travel origins and added projects are not event statistics."
	p.Registrations, p.Attendees, p.Teams, p.Participants = 320, 286, 36, 74
	p.Applications = new(int64(65))
	p.Saved["applications"] = "65"
	p.Saved["applications_source"] = "Synthetic print fixture"
	p.Sats = 5000000
	p.Talks, p.Workshops, p.PanelCount = 30, 9, 3
	for i, value := range []string{"286", "30", "9", "3", "18", "65", "36", "74", "12", "5M"} {
		p.Metrics[i].Value = value
	}
	p.TeamSizes = []reportBar{{Label: "No members", Value: 0}, {Label: "1", Value: 20}, {Label: "2", Value: 7}, {Label: "3", Value: 4}, {Label: "4", Value: 5}, {Label: "5+", Value: 0}}
	for i := range p.Speakers {
		p.Speakers[i].Highlight = i < 8
		if i < 34 {
			p.Speakers[i].ComingFrom = "Nairobi, Kenya"
		}
	}
	for i := range p.Winners {
		p.Winners[i].Portraits = p.Speakers[:4]
	}
	for i := 0; i < 12; i++ {
		p.Winners = append(p.Winners, reportWinner{
			ID: fmt.Sprintf("print-project-%02d", i), Title: fmt.Sprintf("Synthetic winning project %02d", i),
			Awards: "Demo challenge award · honorable mention", Description: "A synthetic prototype used to verify that all award-winning projects and their team members remain together in the PDF.",
			Members: "Example builder one, Example builder two, Example builder three, Example builder four", Portraits: p.Speakers[:4],
		})
	}
	return p
}

func TestReportPaginationPreservesRecords(t *testing.T) {
	p := reportPrintFixture(t)
	var locations []reportSpeakerLocation
	for i, sheet := range p.LocationSheets() {
		limit := 24
		if i == 0 {
			limit = 12
		}
		if sheet.ShowMap != (i == 0) || len(sheet.Locations) > limit {
			t.Fatalf("location sheet %d: %+v", i, sheet)
		}
		locations = append(locations, sheet.Locations...)
	}
	if !reflect.DeepEqual(locations, p.ReportedSpeakerLocations()) {
		t.Fatal("location pagination changed or omitted origins")
	}
	var winners []reportWinner
	for _, sheet := range p.OtherWinnerSheets() {
		if len(sheet) > 12 {
			t.Fatal("too many award cards on one sheet")
		}
		winners = append(winners, sheet...)
	}
	if !reflect.DeepEqual(winners, p.OtherWinners()) {
		t.Fatal("award pagination changed or omitted winning projects")
	}
	var slides []reportWinner
	var speakers, judges []reportPerson
	var panels []reportSession
	for _, section := range p.SlideSections() {
		switch section.Key {
		case "speakers_intro", "timeline_intro", "projects_intro", "geography_intro", "reach_intro":
			t.Fatal("paper spread introduction became an empty slide")
		}
		if len(section.Winners) > 3 || len(section.Speakers) > 6 || len(section.Panels) > 2 || len(section.Judges) > 12 {
			t.Fatal("slide exceeds its roster capacity")
		}
		slides = append(slides, section.Winners...)
		speakers = append(speakers, section.Speakers...)
		judges = append(judges, section.Judges...)
		panels = append(panels, section.Panels...)
	}
	if !reflect.DeepEqual(slides, p.Winners) || !reflect.DeepEqual(speakers, p.HighlightSpeakers()) || !reflect.DeepEqual(judges, p.Judges) || !reflect.DeepEqual(panels, p.Panels) {
		t.Fatal("slide pagination omitted or reordered a roster")
	}
	var items []reportProgramItem
	for _, sheet := range p.ProgramSheets() {
		if len(sheet) > 3 {
			t.Fatal("too many timeline columns")
		}
		for _, day := range sheet {
			if len(day.Items) > 20 {
				t.Fatal("timeline column exceeds its capacity")
			}
			items = append(items, day.Items...)
		}
	}
	var original []reportProgramItem
	for _, day := range p.ProgramDays() {
		original = append(original, day.Items...)
	}
	if !reflect.DeepEqual(items, original) {
		t.Fatal("timeline pagination changed or omitted sessions")
	}
	if pages := (sponsorReportPage{}).LocationSheets(); len(pages) != 1 || !pages[0].ShowMap {
		t.Fatal("empty geography should keep its map without empty continuation sheets")
	}
	for _, count := range []int{0, 1, 12, 13, 24, 25} {
		people := make([]reportPerson, count)
		for i := range people {
			people[i] = reportPerson{ID: fmt.Sprint(i), Highlight: true}
		}
		page := sponsorReportPage{Speakers: people, Judges: people}
		for _, sheets := range [][][]reportPerson{page.HighlightSpeakerSheets(), page.JudgeSheets()} {
			var seen []reportPerson
			for _, sheet := range sheets {
				if len(sheet) > 12 {
					t.Fatal("paper portrait sheet exceeds its capacity")
				}
				seen = append(seen, sheet...)
			}
			if len(seen) != count || (count > 0 && !reflect.DeepEqual(seen, people)) || len(sheets) == 0 {
				t.Fatal("paper portrait pagination changed or omitted people")
			}
		}
	}
}
