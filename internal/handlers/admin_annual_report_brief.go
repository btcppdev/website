package handlers

import "strings"

type annualProjectHighlight struct {
	Event, Tag string
	Project    reportWinner
}

// A short cross-event selection, rather than every prize from every event.
func (p annualReportPage) BriefProjects() []annualProjectHighlight {
	var result []annualProjectHighlight
	seen := map[string]bool{}
	for round := 0; len(result) < 3; round++ {
		added := false
		for _, e := range p.Included() {
			if !e.HackKnown() || round >= len(e.WinningProjects) {
				continue
			}
			w := e.WinningProjects[round]
			if seen[w.ID] {
				continue
			}
			seen[w.ID] = true
			result = append(result, annualProjectHighlight{Event: e.Event.Location, Tag: e.Event.Tag, Project: w})
			added = true
			if len(result) == 3 {
				break
			}
		}
		if !added {
			break
		}
	}
	return result
}
func (p annualReportPage) BriefMedia() []reportMedia {
	media := p.AllMedia()
	if len(media) > 2 {
		media = media[:2]
	}
	return media
}
func (p annualReportPage) BriefHackMetrics() []reportMetric {
	var projects, sats int64
	for _, e := range p.Included() {
		if e.HackKnown() {
			projects += e.Projects
			sats += e.Sats
		}
	}
	var result []reportMetric
	if projects > 0 {
		result = append(result, reportMetric{Label: "submitted projects", Value: compactReportSats(projects)})
	}
	if p.UniqueParticipants > 0 {
		result = append(result, reportMetric{Label: "distinct project members", Value: compactReportSats(p.UniqueParticipants)})
	}
	if sats > 0 {
		result = append(result, reportMetric{Label: "sats sent / claimed", Value: compactReportSats(sats)})
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
