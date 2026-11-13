package handlers

// Bound repeating content before rendering so a report sheet does not grow
// into an unheaded PDF page. Each chunk retains the original order and data.
func reportSheetChunks[T any](items []T, size int) [][]T {
	var pages [][]T
	for len(items) > 0 {
		n := min(size, len(items))
		pages = append(pages, items[:n])
		items = items[n:]
	}
	return pages
}

func (p sponsorReportPage) PanelSheets() [][]reportSession {
	return reportSheetChunks(p.Panels, 3)
}

func (p sponsorReportPage) OtherWinnerSheets() [][]reportWinner {
	return reportSheetChunks(p.OtherWinners(), 12)
}

func reportPeopleSheets(people []reportPerson) [][]reportPerson {
	if len(people) == 0 {
		return [][]reportPerson{nil}
	}
	return reportSheetChunks(people, 12)
}

func (p sponsorReportPage) HighlightSpeakerSheets() [][]reportPerson {
	return reportPeopleSheets(p.HighlightSpeakers())
}

func (p sponsorReportPage) JudgeSheets() [][]reportPerson {
	return reportPeopleSheets(p.Judges)
}

type reportLocationSheet struct {
	ShowMap   bool
	Locations []reportSpeakerLocation
}

func (p sponsorReportPage) LocationSheets() []reportLocationSheet {
	locations := p.ReportedSpeakerLocations()
	n := min(12, len(locations))
	pages := []reportLocationSheet{{ShowMap: true, Locations: locations[:n]}}
	for _, chunk := range reportSheetChunks(locations[n:], 24) {
		pages = append(pages, reportLocationSheet{Locations: chunk})
	}
	return pages
}

func (p sponsorReportPage) ProgramSheets() [][]reportProgramDay {
	var columns []reportProgramDay
	for _, day := range p.ProgramDays() {
		for _, items := range reportSheetChunks(day.Items, 20) {
			column := day
			column.Items = items
			columns = append(columns, column)
		}
	}
	return reportSheetChunks(columns, 3)
}

type reportSlideSection struct {
	reportSection
	Speakers, Judges []reportPerson
	Panels           []reportSession
	Winners          []reportWinner
}

func (p sponsorReportPage) SlideSections() []reportSlideSection {
	var pages []reportSlideSection
	for _, section := range p.Sections {
		// These introduce visual spreads in the paper edition. They are not
		// standalone slides; rendering them here creates nearly empty pages.
		switch section.Key {
		case "speakers_intro", "timeline_intro", "projects_intro", "geography_intro", "reach_intro":
			continue
		}
		var parts []reportSlideSection
		switch section.Key {
		case "program":
			for _, chunk := range reportSheetChunks(p.HighlightSpeakers(), 6) {
				parts = append(parts, reportSlideSection{Speakers: chunk})
			}
		case "panels":
			for _, chunk := range reportSheetChunks(p.Panels, 2) {
				parts = append(parts, reportSlideSection{Panels: chunk})
			}
		case "hackathon":
			for _, chunk := range reportSheetChunks(p.Winners, 3) {
				parts = append(parts, reportSlideSection{Winners: chunk})
			}
		case "judges":
			for _, chunk := range reportSheetChunks(p.Judges, 12) {
				parts = append(parts, reportSlideSection{Judges: chunk})
			}
		}
		if len(parts) == 0 {
			parts = []reportSlideSection{{}}
		}
		for i := range parts {
			parts[i].reportSection = section
			if i > 0 {
				parts[i].Text = ""
			}
			pages = append(pages, parts[i])
		}
	}
	return pages
}
