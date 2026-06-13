package handlers

import (
	"os"
	"testing"

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

func TestHackathonScoreSummaries(t *testing.T) {
	n1, n2 := 1, 2
	ideaHigh, execHigh, impactHigh, rankTwo := 5, 4, 3, 2
	ideaLow, execLow, impactLow, rankOne := 3, 3, 3, 1
	projects := []*types.HackathonProject{
		{ID: "low", Title: "Low Project", ProjectNumber: &n2},
		{ID: "high", Title: "High Project", ProjectNumber: &n1},
		{ID: "empty", Title: "Empty Project"},
	}
	scorecards := []*types.Scorecard{
		{
			ProjectID:      "low",
			IdeaScore:      &ideaLow,
			ExecutionScore: &execLow,
			ImpactScore:    &impactLow,
			Rank:           &rankOne,
		},
		{
			ProjectID:      "high",
			IdeaScore:      &ideaHigh,
			ExecutionScore: &execHigh,
			ImpactScore:    &impactHigh,
			Rank:           &rankTwo,
		},
		{ProjectID: "high", NoShow: true},
	}
	summaries := hackathonScoreSummaries(projects, scorecards)
	if len(summaries) != 3 {
		t.Fatalf("summaries len = %d, want 3", len(summaries))
	}
	if summaries[0].ProjectID != "high" || summaries[0].TotalAverage != "12.0" || summaries[0].NoShows != 1 {
		t.Fatalf("first summary = %+v, want high score with one no-show", summaries[0])
	}
	if summaries[1].ProjectID != "low" || summaries[1].RankAverage != "1.0" || summaries[1].BestRank != "1" {
		t.Fatalf("second summary = %+v, want low project rank data", summaries[1])
	}
	if summaries[2].ProjectID != "empty" || summaries[2].TotalAverage != "-" || summaries[2].Scorecards != 0 {
		t.Fatalf("third summary = %+v, want empty project last", summaries[2])
	}
}
