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
