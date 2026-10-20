package handlers

import (
	"btcpp-web/internal/types"
	"sort"
	"strings"
)

type reportProgramItem struct{ Title, Time, Venue, Kind, URL string }
type reportProgramDay struct {
	Date, Label string
	Items       []reportProgramItem
}

func (p sponsorReportPage) ProgramDays() []reportProgramDay {
	sessions := append([]reportSession(nil), p.Sessions...)
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].Start == nil {
			return false
		}
		if sessions[j].Start == nil {
			return true
		}
		return sessions[i].Start.Before(*sessions[j].Start)
	})
	var days []reportProgramDay
	seen := map[string]bool{}
	for _, s := range sessions {
		if s.Start == nil || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		start := s.Start.In(reportEventLocation(p.Event))
		date := start.Format("2006-01-02")
		if len(days) == 0 || days[len(days)-1].Date != date {
			days = append(days, reportProgramDay{Date: date, Label: start.Format("Mon · 2 Jan")})
		}
		venue := s.Venue
		if name := types.NameVenue(venue); name != "Not Listed Yet" {
			venue = name
		}
		item := reportProgramItem{Title: s.Title, Time: start.Format("15:04"), Venue: venue, Kind: s.Kind, URL: s.WatchURL}
		if item.URL == "" {
			item.URL = s.YouTube
		}
		if s.End != nil {
			item.Time += "–" + s.End.In(reportEventLocation(p.Event)).Format("15:04")
		}
		days[len(days)-1].Items = append(days[len(days)-1].Items, item)
	}
	return days
}
func (p sponsorReportPage) SpeakerSheets() [][]reportPerson {
	var sheets [][]reportPerson
	for start := 0; start < len(p.Speakers); start += 44 {
		end := start + 44
		if end > len(p.Speakers) {
			end = len(p.Speakers)
		}
		sheets = append(sheets, p.Speakers[start:end])
	}
	return sheets
}
func (p sponsorReportPage) ProjectSheets() [][]reportWinner {
	var sheets [][]reportWinner
	for start := 0; start < len(p.Projects); start += 12 {
		end := start + 12
		if end > len(p.Projects) {
			end = len(p.Projects)
		}
		sheets = append(sheets, p.Projects[start:end])
	}
	return sheets
}
func (w reportWinner) ShortDescription() string {
	text := strings.Join(strings.Fields(w.Description), " ")
	r := []rune(text)
	if len(r) > 145 {
		return string(r[:142]) + "…"
	}
	return text
}
