package handlers

import "testing"

func TestAnnualReportFundingScope(t *testing.T) {
	p := annualReportPage{Events: []annualEvent{
		{Included: true, HackCoverage: "records", Sats: 2500000, Event: reportEvent{Location: "Nairobi"}},
		{Included: true, HackCoverage: "unknown", Sats: 9000000},
		{Included: false, HackCoverage: "records", Sats: 8000000},
		{Included: true, HackCoverage: "none", Sats: 0},
	}}
	c := p.FundingChart()
	if len(c.Rows) != 1 || c.DonutTotal() != 2500000 || c.CompactTotal() != "2.5M" {
		t.Fatalf("incorrect payout coverage: %+v", c)
	}
	if c.Donut()[0].CompactValue() != "2.5M" {
		t.Fatal("incorrect legend amount")
	}
	for _, m := range p.BuildingMetrics() {
		if m.Label == "sats sent / claimed" {
			t.Fatal("duplicated payout headline")
		}
	}
}

func TestAnnualReportAutomaticallyIncludesAvailableHackathonRecords(t *testing.T) {
	e := annualEvent{Included: true, HackCoverage: "unknown", Projects: 12, Participants: 25, Winners: 3, Sats: 5000000}
	e.detectHackathonRecords()
	if e.HackCoverage != "automatic" || !e.HackKnown() {
		t.Fatal("existing records still require manual review")
	}
	p := annualReportPage{Events: []annualEvent{e}}
	if len(p.FundingChart().Rows) != 1 || len(p.BuildingMetrics()) == 0 {
		t.Fatal("automatic records missing from annual totals")
	}
	empty := annualEvent{HackCoverage: "unknown"}
	empty.detectHackathonRecords()
	if empty.HackKnown() {
		t.Fatal("missing external results treated as complete")
	}
	none := annualEvent{HackCoverage: "none", Projects: 1}
	none.detectHackathonRecords()
	if none.HackCoverage != "none" {
		t.Fatal("explicit no-hackathon setting silently overridden")
	}
}
