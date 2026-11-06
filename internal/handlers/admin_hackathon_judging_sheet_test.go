package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
)

func TestHackathonJudgingSheet(t *testing.T) {
	two, ten := 2, 10
	page := &HackathonAdminPage{
		Competition: &types.HackathonCompetition{ID: "competition", ConferenceID: "conference", Title: "Seoul Hackathon"},
		Conf:        &types.Conf{Ref: "conference", Tag: "seoul"},
		Projects: []*types.HackathonProject{
			{ID: "ten", Title: "Table ten", Status: getters.ProjectStatusSubmitted, ProjectNumber: &ten},
			{ID: "draft", Title: "Unsubmitted draft", Status: getters.ProjectStatusCreated},
			{ID: "unassigned", Title: "Unassigned project", Status: getters.ProjectStatusSubmitted},
			{ID: "two", Title: "Table two <script>", Status: getters.ProjectStatusAdvanced, ProjectNumber: &two},
			{ID: "hidden", Title: "Hidden project", Status: getters.ProjectStatusHidden},
			nil,
		},
		Awards: []*types.Award{
			{ID: "challenge", Title: "Build & Ship", OptInRequired: true, Status: getters.AwardStatusAvailable},
			{ID: "other", Title: "Other challenge", OptInRequired: true, Status: getters.AwardStatusAwarded},
			{ID: "draft", Title: "Draft challenge", OptInRequired: true, Status: getters.AwardStatusDraft},
			{ID: "overall", Title: "Best overall", Status: getters.AwardStatusAvailable},
			nil,
		},
		AwardOptInsByProject: map[string][]*types.ProjectAwardOptIn{
			"two": {{ProjectID: "two", AwardID: "challenge"}},
		},
	}
	tmpl, err := template.ParseFiles("../../templates/admin/hackathon_judging_sheet.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"Seoul Hackathon", "Idea", "Execution", "General Effect", "Notes", "Build &amp; Ship", "Other challenge", "TBA", "/seoul/admin/hackathon/judging", "window.print()"} {
		if !strings.Contains(body, want) {
			t.Errorf("judging sheet missing %q", want)
		}
	}
	for _, unwanted := range []string{"Unsubmitted draft", "Hidden project", "Draft challenge", "Best overall", "<script>"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("judging sheet contains %q", unwanted)
		}
	}
	if twoAt, tenAt, unassignedAt := strings.Index(body, "Table two &lt;script&gt;"), strings.Index(body, "Table ten"), strings.Index(body, "Unassigned project"); twoAt < 0 || twoAt >= tenAt || tenAt >= unassignedAt {
		t.Fatal("projects must be sorted numerically by table with unassigned projects last")
	}
	if got := strings.Count(body, `aria-label="Opted in"`); got != 1 {
		t.Errorf("checked opt-ins = %d, want 1", got)
	}
	if got := strings.Count(body, `aria-label="Not opted in"`); got != 5 {
		t.Errorf("unchecked opt-ins = %d, want 5", got)
	}
	if got := strings.Count(body, "<td></td>"); got != 12 {
		t.Errorf("blank score and notes cells = %d, want 12", got)
	}
	if page.Projects[0].ID != "ten" {
		t.Fatal("rendering must not reorder the source project list")
	}

	page.Awards = nil
	out.Reset()
	if err := tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "judging-sheet--no-challenges") || strings.Contains(out.String(), "sheet-checkbox") {
		t.Fatal("a hackathon without opt-in challenges should still have a printable score table")
	}

	page.Projects = nil
	out.Reset()
	if err := tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No submitted projects yet.") {
		t.Fatal("empty sheet must explain why there are no project rows")
	}
}

func TestHackathonJudgingSheetURLs(t *testing.T) {
	competition := &types.HackathonCompetition{ID: "competition", ConferenceID: "conference"}
	page := &HackathonAdminPage{}
	if got := page.JudgingSheetURL(competition); got != "/admin/hackathons/competition/judging/print" {
		t.Fatalf("global judging sheet URL = %q", got)
	}
	page.Conf = &types.Conf{Ref: "conference", Tag: "seoul"}
	if got := page.JudgingSheetURL(competition); got != "/seoul/admin/hackathon/judging/print" {
		t.Fatalf("conference judging sheet URL = %q", got)
	}
}
