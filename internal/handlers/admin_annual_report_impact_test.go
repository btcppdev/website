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

func TestAnnualReportIncompleteHackathonExcluded(t *testing.T) {
	e := annualEvent{Included: true, Eligible: true, HackCoverage: "incomplete", Projects: 10, Participants: 20, Sats: 1000000, Talks: 12, Registrations: 150, WinningProjects: []reportWinner{{ID: "partial", Title: "Partial winner"}}}
	e.detectHackathonRecords()
	p := annualReportPage{RegistrationBased: true, Events: []annualEvent{e}}
	if e.HackKnown() || len(p.FundingChart().Rows) != 0 || len(p.BriefProjects()) != 0 || len(p.BuildingMetrics()) != 0 {
		t.Fatal("incomplete records leaked into hackathon rollup")
	}
	if p.ParticipationTotal() != 150 || p.ProgramChart().Rows[0].Value != 12 {
		t.Fatal("non-hackathon statistics were excluded")
	}
	if annualEventStats(e)[2].Value != "—" {
		t.Fatal("partial payout shown as complete")
	}
}

func TestAnnualReportPrizeBasisPreservesFrozenEditions(t *testing.T) {
	p := annualReportPage{Events: []annualEvent{{Included: true, HackCoverage: "records", Sats: 3000}}}
	if p.FundingChart().Unit != "sats sent / claimed" || annualEventStats(p.Events[0])[2].Label != "sats sent / claimed" {
		t.Fatal("legacy payout snapshot relabeled")
	}
	p.AwardBased = true
	p.Events[0].AwardBased = true
	if p.FundingChart().Unit != "sats awarded" || annualEventStats(p.Events[0])[2].Label != "sats awarded" {
		t.Fatal("live award totals mislabeled")
	}
	for _, m := range p.BuildingMetrics() {
		if m.Label == "sats awarded" {
			t.Fatal("duplicate prize headline")
		}
	}
}

func TestAnnualReportOfferedPrizeLabels(t *testing.T) {
	p := annualReportPage{OfferedBased: true, AwardBased: true, Events: []annualEvent{{OfferedBased: true, AwardBased: true, Included: true, HackCoverage: "automatic", Sats: 1000000}}}
	if p.FundingChart().Unit != "sats offered in prizes" || annualEventStats(p.Events[0])[2].Label != "sats offered in prizes" {
		t.Fatal("offered prizes mislabeled as payouts or awarded prizes")
	}
}
