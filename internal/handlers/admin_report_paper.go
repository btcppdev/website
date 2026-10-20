package handlers

import "strconv"

// PublicEvidence is a dated source snapshot, separate from private database metrics.
type reportEvidence struct {
	Label, Value, Note, Source string
}

func (p sponsorReportPage) Section(key string) reportSection {
	for _, s := range p.Sections {
		if s.Key == key {
			return s
		}
	}
	return reportSection{}
}
func (p sponsorReportPage) HeadlineEvidence() []reportEvidence {
	result := []reportEvidence{
		{Label: "program speakers", Value: strconv.Itoa(p.SpeakerCount())},
		{Label: "workshops held", Value: "—"},
		{Label: "hackathon projects", Value: "—"},
		{Label: "hackathon participants", Value: "—"},
	}
	if p.ProgramReviewed && !p.Preview {
		result[1].Value = strconv.FormatInt(p.Workshops, 10)
	} else if program := p.ProgramSummary(); program != nil {
		result[1].Label = "scheduled workshops"
		result[1].Value = strconv.FormatInt(program.Rows[1].Value, 10)
	}
	if p.Preview {
		for _, e := range p.PublicEvidence {
			if e.Label == "projects in the public gallery" {
				result[2] = e
				result[2].Label = "listed hackathon projects"
			}
		}
	} else {
		result[2].Value = strconv.FormatInt(p.Teams, 10)
		result[3].Value = strconv.FormatInt(p.Participants, 10)
	}
	return result
}
func (p sponsorReportPage) ReadyCharts() []reportChart {
	var ready []reportChart
	for _, c := range p.Charts() {
		if len(c.Rows) > 0 {
			ready = append(ready, c)
		}
	}
	return ready
}
func (p sponsorReportPage) ChartFor(kind string) *reportChart {
	for _, c := range p.ReadyCharts() {
		if c.Kind == kind {
			return &c
		}
	}
	return nil
}
func (p sponsorReportPage) PublishedPrizeChart() *reportChart {
	if !p.Preview || p.EvidenceDate == "" {
		return nil
	}
	c := reportChart{Title: "The overall prize awards", Kind: "published-prizes", Unit: "sats awarded",
		Note: "Published overall awards only. This is not a verified payout total."}
	for _, w := range p.Winners {
		if w.AwardedSats > 0 {
			c.Rows = append(c.Rows, reportBar{Label: w.Title, Detail: w.Awards, Value: w.AwardedSats})
		}
	}
	if len(c.Rows) == 0 {
		return nil
	}
	return &c
}
func (p sponsorReportPage) HighlightSpeakers() []reportPerson {
	var result []reportPerson
	for _, person := range p.Speakers {
		if person.Highlight {
			result = append(result, person)
		}
	}
	return result
}
func (c reportChart) CompactTotal() string { return compactReportSats(c.DonutTotal()) }
