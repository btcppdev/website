package handlers

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

func (p sponsorReportPage) ProgramSummary() *reportChart {
	if p.Preview && p.PublicProgramSource == "" {
		return nil
	}
	c := reportChart{Kind: "program", Title: "The published program", Unit: "scheduled sessions", Note: "Accepted program entries, not confirmation of delivery. Talks include keynotes. Source: " + p.PublicProgramSource, Rows: []reportBar{{Label: "Talks & keynotes"}, {Label: "Workshops"}, {Label: "Panels"}}}
	if !p.Preview {
		c.Note = "Accepted, non-archived program entries from event records; not confirmation of delivery."
	}
	seen := map[string]bool{}
	for _, s := range p.Sessions {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		switch s.Kind {
		case "talk", "keynote":
			c.Rows[0].Value++
		case "workshop":
			c.Rows[1].Value++
		case "panel":
			c.Rows[2].Value++
		}
	}
	if c.DonutTotal() == 0 {
		return nil
	}
	return &c
}
func (p sponsorReportPage) SpeakerCount() int {
	seen := map[string]bool{}
	for _, s := range p.Speakers {
		seen[s.ID] = true
	}
	return len(seen)
}
func (p sponsorReportPage) RecordingCount() int {
	seen := map[string]bool{}
	for _, s := range p.Sessions {
		if s.WatchURL != "" || s.YouTube != "" || s.X != "" {
			seen[s.ID] = true
		}
	}
	return len(seen)
}

type reportSpeakerLocation struct {
	ID, Name, Place, Latitude, Longitude, Source string
	X, Y                                         float64
	Mapped                                       bool
}

func (p sponsorReportPage) SpeakerLocations() []reportSpeakerLocation {
	var result []reportSpeakerLocation
	for _, s := range p.Speakers {
		pre := "speaker_location_" + s.ID + "_"
		l := reportSpeakerLocation{ID: s.ID, Name: s.Name, Place: p.Saved[pre+"place"], Latitude: p.Saved[pre+"lat"], Longitude: p.Saved[pre+"lon"], Source: p.Saved[pre+"source"]}
		// The event-specific speaker form is the default source. Manual report
		// overrides remain available to resolve ambiguous free-text responses.
		if l.Place == "" && strings.TrimSpace(s.ComingFrom) != "" && !strings.EqualFold(strings.TrimSpace(s.ComingFrom), "unknown") {
			l.Place = strings.TrimSpace(s.ComingFrom)
			l.Source = "Speaker's event form · traveling from"
			// Old coordinates must not attach to a changed/defaulted place.
			l.Latitude, l.Longitude = "", ""
		}
		lat, e1 := strconv.ParseFloat(l.Latitude, 64)
		lon, e2 := strconv.ParseFloat(l.Longitude, 64)
		if l.Place != "" && l.Source != "" && e1 == nil && e2 == nil && validReportCoords(lat, lon) {
			center, cx := 30.0, 180.0
			if math.Cos((lon-center)*math.Pi/180) < 0 {
				center, cx = -150, 540
			}
			l.X = cx + 155*math.Cos(lat*math.Pi/180)*math.Sin((lon-center)*math.Pi/180)
			l.Y = 175 - 155*math.Sin(lat*math.Pi/180)
			l.Mapped = true
		}
		result = append(result, l)
	}
	return result
}
func (p sponsorReportPage) MappedSpeakers() []reportSpeakerLocation {
	var out []reportSpeakerLocation
	for _, l := range p.SpeakerLocations() {
		if l.Mapped {
			out = append(out, l)
		}
	}
	return out
}
func (p sponsorReportPage) ReportedSpeakerLocations() []reportSpeakerLocation {
	var out []reportSpeakerLocation
	for _, l := range p.SpeakerLocations() {
		if l.Place != "" {
			out = append(out, l)
		}
	}
	return out
}
func validReportCoords(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
func saveReportSpeakerLocation(form url.Values, saved map[string]string, id string) error {
	pre := "speaker_location_" + id + "_"
	has := false
	for _, suffix := range []string{"place", "lat", "lon", "source"} {
		v := strings.TrimSpace(form.Get(pre + suffix))
		if len(v) > 1000 {
			return fmt.Errorf("speaker location too long")
		}
		saved[pre+suffix] = v
		has = has || v != ""
	}
	if !has {
		return nil
	}
	// A form response is useful even before its free-text place is geocoded.
	if saved[pre+"place"] != "" && saved[pre+"source"] != "" && saved[pre+"lat"] == "" && saved[pre+"lon"] == "" {
		return nil
	}
	lat, e1 := strconv.ParseFloat(saved[pre+"lat"], 64)
	lon, e2 := strconv.ParseFloat(saved[pre+"lon"], 64)
	if saved[pre+"place"] == "" || saved[pre+"source"] == "" || e1 != nil || e2 != nil || !validReportCoords(lat, lon) {
		return fmt.Errorf("speaker locations need a place, valid latitude/longitude and confirmation source")
	}
	return nil
}
