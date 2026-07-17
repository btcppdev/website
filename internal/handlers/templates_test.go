package handlers

import (
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestLoadTemplates(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	for _, name := range []string{"hackathon.tmpl", "hackathon_judging.tmpl", "hackathon_project.tmpl", "hackathon_schedule.tmpl", "admin/hackathon_projects.tmpl", "admin/hackathon_judging.tmpl", "admin/hackathon_scores.tmpl", "admin/hackathon_awards.tmpl"} {
		if ctx.TemplateCache.Lookup(name) == nil {
			t.Fatalf("template %s was not loaded", name)
		}
	}
}

func TestHackathonRichTextHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "allowed formatting",
			input: `<p>Hello <strong>world</strong><br><a href="https://example.com" onclick="bad()">link</a></p>`,
			want:  `<p>Hello <strong>world</strong><br><a href="https://example.com" rel="noopener noreferrer">link</a></p>`,
		},
		{
			name:  "unsafe tags removed",
			input: `<p>Safe</p><script>alert("bad")</script><style>body{display:none}</style>`,
			want:  `<p>Safe</p>`,
		},
		{
			name:  "unsafe links lose href",
			input: `<a href="javascript:alert(1)">bad</a> <a href="/hackathons/test">good</a>`,
			want:  `<a>bad</a> <a href="/hackathons/test" rel="noopener noreferrer">good</a>`,
		},
		{
			name:  "plain text is escaped",
			input: `2 < 3 & 4 > 1`,
			want:  `2 &lt; 3 &amp; 4 &gt; 1`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(hackathonRichTextHTML(tt.input)); got != tt.want {
				t.Fatalf("hackathonRichTextHTML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHackathonDescriptionHTML(t *testing.T) {
	markdown := string(hackathonDescriptionHTML("A **bold** [link](https://example.com).\n\n<script>bad()</script>", getters.CompetitionDescriptionFormatMarkdown))
	for _, want := range []string{
		"<strong>bold</strong>",
		`<a href="https://example.com" rel="noopener noreferrer">link</a>`,
		"&amp;lt;script&amp;gt;bad()&amp;lt;/script&amp;gt;",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown description missing %q in %q", want, markdown)
		}
	}
	if strings.Contains(markdown, "<script>") {
		t.Fatalf("markdown description rendered raw script: %q", markdown)
	}

	plain := string(hackathonDescriptionHTML("2 < 3\nnext", getters.CompetitionDescriptionFormatPlain))
	if plain != "2 &lt; 3<br>next" {
		t.Fatalf("plain description = %q", plain)
	}

	html := string(hackathonDescriptionHTML(`<p><em>ok</em></p><script>bad()</script>`, getters.CompetitionDescriptionFormatHTML))
	if html != "<p><em>ok</em></p>" {
		t.Fatalf("html description = %q", html)
	}
}

func TestHackathonScoreSummaries(t *testing.T) {
	n1, n2 := 1, 2
	rankOne, rankTwo := 1, 2
	projects := []*types.HackathonProject{
		{ID: "low", Title: "Low Project", ProjectNumber: &n2},
		{ID: "high", Title: "High Project", ProjectNumber: &n1},
		{ID: "empty", Title: "Empty Project"},
	}
	events := []*types.JudgeEvent{{ID: "expo", PlaybookType: getters.JudgeTypeExpo, RankLimit: 4}}
	scorecards := []*types.Scorecard{
		{
			ProjectID:    "low",
			JudgeEventID: "expo",
			Rank:         &rankTwo,
		},
		{
			ProjectID:    "high",
			JudgeEventID: "expo",
			Rank:         &rankOne,
		},
	}
	summaries := hackathonScoreSummaries(projects, scorecards, events)
	if len(summaries) != 3 {
		t.Fatalf("summaries len = %d, want 3", len(summaries))
	}
	if summaries[0].ProjectID != "high" || summaries[0].Points != 4 {
		t.Fatalf("first summary = %+v, want high score", summaries[0])
	}
	if summaries[1].ProjectID != "low" || summaries[1].Points != 3 || summaries[1].RankAverage != "2.0" {
		t.Fatalf("second summary = %+v, want low project rank data", summaries[1])
	}
	if summaries[2].ProjectID != "empty" || summaries[2].PointsLabel != "-" || summaries[2].Scorecards != 0 {
		t.Fatalf("third summary = %+v, want empty project last", summaries[2])
	}
}

func TestHackathonAdvancedSelections(t *testing.T) {
	n1, n2, n3 := 1, 2, 3
	rankOne, rankTwo := 1, 2
	projects := []*types.HackathonProject{
		{ID: "winner", Title: "Winner", ProjectNumber: &n1, Status: getters.ProjectStatusSubmitted},
		{ID: "runner-up", Title: "Runner Up", ProjectNumber: &n2, Status: getters.ProjectStatusAdvanced},
		{ID: "created", Title: "Created", ProjectNumber: &n3, Status: getters.ProjectStatusCreated},
	}
	events := []*types.JudgeEvent{
		{ID: "expo", PlaybookType: getters.JudgeTypeExpo, RankLimit: 4},
		{ID: "finals", PlaybookType: getters.JudgeTypeFinals, RankLimit: 4},
	}
	scorecards := []*types.Scorecard{
		{ProjectID: "winner", JudgeEventID: "expo", Rank: &rankOne},
		{ProjectID: "runner-up", JudgeEventID: "expo", Rank: &rankTwo},
		{ProjectID: "created", JudgeEventID: "expo", Rank: &rankOne},
		{ProjectID: "runner-up", JudgeEventID: "finals", Rank: &rankOne},
	}

	expoAdvanced := hackathonAdvancedSelections(projects, scorecards, events, hackathonScoreModeExpo, 2)
	if len(expoAdvanced) != 2 || expoAdvanced[0].ID != "winner" || expoAdvanced[1].ID != "runner-up" {
		t.Fatalf("expo advanced = %+v, want winner then runner-up", expoAdvanced)
	}

	finalsAdvanced := hackathonAdvancedSelections(projects, scorecards, events, hackathonScoreModeFinals, 2)
	if len(finalsAdvanced) != 1 || finalsAdvanced[0].ID != "runner-up" {
		t.Fatalf("finals advanced = %+v, want runner-up only", finalsAdvanced)
	}

	if advanced := hackathonAdvancedSelections(projects, scorecards, events, hackathonScoreModeExpo, 0); len(advanced) != 0 {
		t.Fatalf("zero advance count returned %+v, want empty", advanced)
	}
}

func TestCurrentJudgeEvents(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	before := now.Add(-time.Hour)
	after := now.Add(time.Hour)

	manual := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeManual}
	manualEvents := []*types.JudgeEvent{
		{ID: "pending", State: getters.JudgeEventStatePending},
		{ID: "open", State: getters.JudgeEventStateOpen},
	}
	if got := currentJudgeEvents(manual, manualEvents, now); len(got) != 1 || got[0].ID != "open" {
		t.Fatalf("manual current events = %+v, want open", got)
	}

	automatic := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeAutomatic}
	autoEvents := []*types.JudgeEvent{
		{ID: "future", StartsAt: &after},
		{ID: "current", StartsAt: &before, EndsAt: &after},
	}
	if got := currentJudgeEvents(automatic, autoEvents, now); len(got) != 1 || got[0].ID != "current" {
		t.Fatalf("automatic current events = %+v, want current", got)
	}

	endedAt := now.Add(-time.Minute)
	closed := []*types.JudgeEvent{{ID: "done", StartsAt: &before, EndsAt: &endedAt}}
	if got := currentJudgeEvents(automatic, closed, now); len(got) != 0 {
		t.Fatalf("closed automatic event = %+v, want none", got)
	}
}
