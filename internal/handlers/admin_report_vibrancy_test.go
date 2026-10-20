package handlers

import "testing"

func TestReportVibrancyCoverage(t *testing.T) {
	p := reportFixturePage(t)
	days := p.ProgramDays()
	if len(days) != 3 || len(days[0].Items) != 17 || len(days[1].Items) != 13 || len(days[2].Items) != 14 {
		t.Fatal("timeline omitted or duplicated scheduled sessions")
	}
	if days[0].Items[0].Time != "09:45–10:00" || days[0].Items[0].Venue != "Main Stage" {
		t.Fatal("timeline lost local time or stage")
	}
	if len(p.SpeakerSheets()) != 1 || len(p.SpeakerSheets()[0]) != 44 {
		t.Fatal("speaker mosaic incomplete")
	}
	seen := map[string]bool{}
	for _, sheet := range p.ProjectSheets() {
		if len(sheet) > 12 {
			t.Fatal("oversized project sheet")
		}
		for _, project := range sheet {
			if seen[project.ID] {
				t.Fatal("duplicate gallery project")
			}
			seen[project.ID] = true
		}
	}
	if len(seen) != 36 {
		t.Fatal("project wall incomplete")
	}
	p.Sessions = append(p.Sessions, p.Sessions[0], reportSession{ID: "unscheduled"})
	if len(p.ProgramDays()[0].Items) != 17 {
		t.Fatal("timeline duplicate or undated session")
	}
}
