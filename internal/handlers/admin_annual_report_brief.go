package handlers

import (
	"fmt"
	"strings"
)

type annualProjectHighlight struct {
	Event, Tag string
	EventImage string
	Project    reportWinner
	Story      annualProjectStory
}

// Include every finalized overall first-place project, including ties. Sponsor
// challenge winners do not become overall winners because of a prize title.
func (p annualReportPage) BriefProjects() []annualProjectHighlight {
	var result []annualProjectHighlight
	seen := map[string]bool{}
	for _, e := range p.Included() {
		if !e.HackKnown() {
			continue
		}
		for _, w := range e.WinningProjects {
			key := e.Event.Tag + ":" + w.ID
			if w.PlaceRank() != 1 || seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, annualProjectHighlight{Event: e.Event.Location, Tag: e.Event.Tag, EventImage: e.Event.LeadImage, Project: w, Story: p.projectStory(w)})
		}
	}
	return result
}

type annualProjectHighlightPage struct {
	Folio    string
	Projects []annualProjectHighlight
}

func (p annualReportPage) ProjectHighlightPages() []annualProjectHighlightPage {
	pages := []annualProjectHighlightPage{{}}
	for _, project := range p.BriefProjects() {
		if len(pages[len(pages)-1].Projects) == 4 {
			pages = append(pages, annualProjectHighlightPage{})
		}
		pages[len(pages)-1].Projects = append(pages[len(pages)-1].Projects, project)
	}
	for i := range pages {
		pages[i].Folio = fmt.Sprintf("%02d / %02d", 7+(len(p.Included())+2)/3+i, p.ReportPageCount())
	}
	return pages
}
func (p annualReportPage) BriefMedia() []reportMedia {
	media := p.AllMedia()
	if len(media) > 2 {
		media = media[:2]
	}
	return media
}
func (p annualReportPage) BriefHackMetrics() []reportMetric {
	var projects, participations, sats int64
	var estimated bool
	for _, e := range p.Included() {
		if e.HackKnown() {
			projects += e.Projects
			participations += e.Participants
			estimated = estimated || e.ParticipantsEstimated
			sats += e.Sats
		}
	}
	var result []reportMetric
	if projects > 0 {
		result = append(result, reportMetric{Label: "submitted projects", Value: compactReportSats(projects)})
	}
	if participations > 0 {
		result = append(result, reportMetric{Label: "hackathon participations", Value: approximateReportCount(participations, estimated)})
	}
	if sats > 0 {
		result = append(result, reportMetric{Label: p.PrizeUnit(), Value: p.PrizeTotalDisplay()})
	}
	return result
}

// Leave editorial prompts in the editor, not the sponsor document. Actual prose
// is never truncated; longer custom copy can flow onto another printed page.
func (p annualReportPage) BriefSection(key string) reportSection {
	s := p.Section(key)
	var paragraphs []string
	for _, part := range strings.Split(s.Text, "\n\n") {
		trimmed := strings.TrimSpace(part)
		if strings.HasPrefix(trimmed, "[Add ") || strings.HasPrefix(trimmed, "[Highlight ") || strings.HasPrefix(trimmed, "[Describe ") {
			continue
		}
		paragraphs = append(paragraphs, part)
	}
	s.Text = strings.Join(paragraphs, "\n\n")
	return s
}
