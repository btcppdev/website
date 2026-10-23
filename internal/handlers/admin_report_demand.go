package handlers

import (
	"fmt"
	"strconv"
	"strings"
)

type reportStageDemand struct {
	Count                      int64
	Label, Title, Ratio, Scope string
}

func (d reportStageDemand) Display() string { return compactReportSats(d.Count) }
func verifiedReportApplications(saved map[string]string) *int64 {
	if strings.TrimSpace(saved["applications_source"]) == "" {
		return nil
	}
	n, err := reportOptionalCount(saved["applications"])
	if err != nil {
		return nil
	}
	return n
}
func stageDemand(applications *int64, proposals, sessions int64) *reportStageDemand {
	d := reportStageDemand{Count: proposals, Label: "program proposals", Title: "Building the program"}
	if applications != nil {
		d.Count = *applications
		d.Label = "speaking applications"
		d.Title = "Interest in the stage"
		if sessions > 0 && d.Count > sessions {
			d.Ratio = fmt.Sprintf("%.1f applications per program session", float64(d.Count)/float64(sessions))
		}
	}
	if d.Count <= 0 {
		return nil
	}
	return &d
}
func (p sponsorReportPage) StageDemand() *reportStageDemand {
	if p.Preview && !p.Synthetic && !p.DemoApplications {
		return nil
	}
	var sessions int64
	if c := p.ProgramSummary(); c != nil {
		sessions = c.DonutTotal()
	}
	d := stageDemand(verifiedReportApplications(p.Saved), p.Proposals, sessions)
	if d != nil && p.DemoApplications {
		d.Label = "illustrative " + d.Label
	}
	return d
}
func (p annualReportPage) StageDemand() *reportStageDemand {
	var applications, proposals, sessions, reviewed int64
	for _, e := range p.Included() {
		proposals += e.Proposals
		if e.Applications != nil {
			applications += *e.Applications
			sessions += e.Sessions()
			reviewed++
		}
	}
	if reviewed > 0 {
		d := stageDemand(&applications, 0, sessions)
		if d != nil {
			d.Scope = "Across " + strconv.FormatInt(reviewed, 10) + " reviewed events"
			if reviewed == 1 {
				d.Scope = "Across 1 reviewed event"
			}
		}
		return d
	}
	return stageDemand(nil, proposals, 0)
}
