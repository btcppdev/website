package handlers

import "testing"

func TestReportStageDemand(t *testing.T) {
	p := sponsorReportPage{Proposals: 50, Saved: map[string]string{}, Sessions: []reportSession{{ID: "1", Kind: "talk"}, {ID: "1", Kind: "talk"}, {ID: "2", Kind: "workshop"}}}
	if d := p.StageDemand(); d == nil || d.Label != "program proposals" || d.Ratio != "" {
		t.Fatal("unverified proposals presented as applications", d)
	}
	p.Saved["applications"] = "10"
	if d := p.StageDemand(); d.Label != "program proposals" {
		t.Fatal("unattributed override accepted")
	}
	p.Saved["applications_source"] = "Reviewed CFP export, excluding invitations"
	if d := p.StageDemand(); d.Count != 10 || d.Ratio != "5.0 applications per program session" {
		t.Fatal("bad application demand", d)
	}
	p.Saved["applications"] = "0"
	if p.StageDemand() != nil {
		t.Fatal("verified zero fell back to proposal records")
	}
	p.Saved["applications"] = "10"
	p.Preview = true
	if p.StageDemand() != nil {
		t.Fatal("demo application figure leaked without explicit flag")
	}
	p.DemoApplications = true
	if d := p.StageDemand(); d.Label != "illustrative speaking applications" {
		t.Fatal("demo not labeled", d)
	}
}
func TestAnnualReportStageDemandScope(t *testing.T) {
	n := int64(30)
	p := annualReportPage{Events: []annualEvent{{Included: true, Applications: &n, Proposals: 40, Talks: 10}, {Included: true, Proposals: 90, Talks: 20}, {Included: false, Applications: &n, Talks: 1}}}
	d := p.StageDemand()
	if d.Count != 30 || d.Ratio != "3.0 applications per program session" || d.Scope != "Across 1 reviewed event" {
		t.Fatal("mixed verified and unverified demand", d)
	}
	p.Events[0].Applications = nil
	if d = p.StageDemand(); d.Count != 130 || d.Label != "program proposals" || d.Ratio != "" {
		t.Fatal("bad proposal fallback", d)
	}
}
