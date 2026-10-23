package handlers

// Only reconciled event payouts contribute to the funding chart. Announced
// prize pools and per-project awarded amounts are not added to distributions.
func (p annualReportPage) FundingChart() reportChart {
	c := reportChart{Kind: "program", Unit: "sats sent / claimed"}
	for _, e := range p.Included() {
		if e.HackKnown() && e.Sats > 0 {
			c.Rows = append(c.Rows, reportBar{Label: e.Event.Location, Value: e.Sats})
		}
	}
	return c
}
func (s reportSlice) CompactValue() string { return compactReportSats(s.Value) }
func (p annualReportPage) BuildingMetrics() []reportMetric {
	metrics := p.BriefHackMetrics()
	var out []reportMetric
	for _, m := range metrics {
		if m.Label != "sats sent / claimed" {
			out = append(out, m)
		}
	}
	return out
}

// Existing records can be included without an extra manual review flag. This does
// not assert completeness or turn announced awards into recorded distributions.
func (e *annualEvent) detectHackathonRecords() {
	if e.HackCoverage != "unknown" && e.HackCoverage != "" {
		return
	}
	if e.Projects > 0 || e.Participants > 0 || e.Sats > 0 || e.Winners > 0 || e.Challenges > 0 {
		e.HackCoverage = "automatic"
	}
}
func (e annualEvent) DataStatus() string {
	if !e.Eligible {
		return "Outside the completion cutoff; shown editorially where applicable."
	}
	if !e.Included {
		return "Excluded from this draft. Select this event or use Include all completed events."
	}
	status := "Registrations and program loaded from the database. "
	if e.HackKnown() {
		status += "Available hackathon records included automatically. "
		if e.Sats == 0 {
			status += "No sent/claimed sats payouts recorded; funding chart has no amount for this event. "
		}
	} else if e.HackCoverage == "none" {
		status += "No hackathon. "
	} else {
		status += "No hackathon records found; external results may still be needed. "
	}
	if len(e.Media) == 0 {
		status += "No recording analytics saved yet: use the bulk analytics refresh below."
	} else {
		status += "Saved recording analytics included."
	}
	if len(e.Posts) == 0 {
		status += " No X posts approved for attribution yet."
	}
	return status
}
