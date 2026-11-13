package handlers

// Live annual reports count prizes offered. Frozen editions retain their saved basis.
func (p annualReportPage) FundingChart() reportChart {
	c := reportChart{Kind: "program", Unit: p.PrizeUnit()}
	for _, e := range p.Included() {
		if e.HackKnown() && e.Sats > 0 {
			c.Rows = append(c.Rows, reportBar{Label: e.Event.Location, Value: e.Sats})
		}
	}
	return c
}

func (p annualReportPage) PrizeTotalDisplay() string {
	var total int64
	var estimated bool
	for _, e := range p.Included() {
		if e.HackKnown() && e.Sats > 0 {
			total += e.Sats
			estimated = estimated || e.PrizesEstimated
		}
	}
	return approximateReportCount(total, estimated)
}
func (s reportSlice) CompactValue() string { return compactReportSats(s.Value) }
func (p annualReportPage) BuildingMetrics() []reportMetric {
	metrics := p.BriefHackMetrics()
	var out []reportMetric
	for _, m := range metrics {
		if m.Label != p.PrizeUnit() {
			out = append(out, m)
		}
	}
	return out
}

// Existing records can be included without an extra manual review flag. This does
// not assert completeness; prize offers do not require finalized results.
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
		return "Excluded from this draft. Select this event or use Restore all completed events."
	}
	status := "Registrations and program loaded from the database. "
	if e.HackathonRetrospective != nil && e.HackKnown() {
		status += "Hackathon totals and named winning projects come from the published Insider retrospective. Participant count is approximate; prize amounts and a complete winner roster are not supplied by that source. "
		if e.PrizesEstimated {
			status += "The prize pool uses the organizer’s estimate of approximately 8M sats, supplied 13 November 2026. "
		}
	} else if e.HackKnown() {
		status += "Available hackathon records included automatically. "
		if e.Sats == 0 {
			status += "No qualifying sat prize amounts recorded; funding chart has no amount for this event. "
		}
	} else if e.HackCoverage == "incomplete" || e.HackCoverage == "excluded" {
		status += "Incomplete historical hackathon records: excluded from project, participant, winner and prize totals. Other event statistics remain included. "
	} else if e.HackCoverage == "none" {
		status += "No hackathon. "
	} else {
		status += "No hackathon records found; external results may still be needed. "
	}
	if len(e.Media) == 0 {
		status += "Recording analytics will be collected automatically."
	} else {
		status += "Saved recording analytics included."
	}
	if len(e.Posts) == 0 {
		status += " No matching X posts in the latest snapshot."
	}
	return status
}

func (p annualReportPage) PrizeUnit() string {
	if p.OfferedBased {
		return "sats offered in prizes"
	}
	if p.AwardBased {
		return "sats awarded"
	}
	return "sats sent / claimed"
}
func (e annualEvent) PrizeUnit() string {
	if e.OfferedBased {
		return "sats offered in prizes"
	}
	if e.AwardBased {
		return "sats awarded"
	}
	return "sats sent / claimed"
}
