package handlers

import (
	"os"
	"testing"

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
		{ProjectID: "high", NoShow: true},
	}
	summaries := hackathonScoreSummaries(projects, scorecards, events)
	if len(summaries) != 3 {
		t.Fatalf("summaries len = %d, want 3", len(summaries))
	}
	if summaries[0].ProjectID != "high" || summaries[0].Points != 4 || summaries[0].NoShows != 1 {
		t.Fatalf("first summary = %+v, want high score with one no-show", summaries[0])
	}
	if summaries[1].ProjectID != "low" || summaries[1].Points != 3 || summaries[1].RankAverage != "2.0" || summaries[1].BestRank != "2" {
		t.Fatalf("second summary = %+v, want low project rank data", summaries[1])
	}
	if summaries[2].ProjectID != "empty" || summaries[2].PointsLabel != "-" || summaries[2].Scorecards != 0 {
		t.Fatalf("third summary = %+v, want empty project last", summaries[2])
	}
}

func TestHackathonFinalistSelections(t *testing.T) {
	n1, n2, n3 := 1, 2, 3
	rankOne, rankTwo := 1, 2
	projects := []*types.HackathonProject{
		{ID: "winner", Title: "Winner", ProjectNumber: &n1, Status: getters.ProjectStatusSubmitted},
		{ID: "runner-up", Title: "Runner Up", ProjectNumber: &n2, Status: getters.ProjectStatusShipped},
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

	expoFinalists := hackathonFinalistSelections(projects, scorecards, events, hackathonScoreModeExpo, 2)
	if len(expoFinalists) != 2 || expoFinalists[0].ID != "winner" || expoFinalists[1].ID != "runner-up" {
		t.Fatalf("expo finalists = %+v, want winner then runner-up", expoFinalists)
	}

	finalsFinalists := hackathonFinalistSelections(projects, scorecards, events, hackathonScoreModeFinals, 2)
	if len(finalsFinalists) != 1 || finalsFinalists[0].ID != "runner-up" {
		t.Fatalf("finals finalists = %+v, want runner-up only", finalsFinalists)
	}

	if finalists := hackathonFinalistSelections(projects, scorecards, events, hackathonScoreModeExpo, 0); len(finalists) != 0 {
		t.Fatalf("zero finalist count returned %+v, want empty", finalists)
	}
}
