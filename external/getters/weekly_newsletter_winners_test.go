package getters

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestWeeklyNewsletterWinnersForRecentlyEndedEvents(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	end := time.Now().UTC().Truncate(time.Second)
	start := end.AddDate(0, 0, -7)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	wanted := map[string]bool{}
	testTags := map[string]bool{}
	for _, tc := range []struct {
		name                     string
		ended                    time.Time
		finalized, gallery, want bool
	}{
		{"recent", end.AddDate(0, 0, -2), true, true, true},
		{"window-start", start, true, true, true},
		{"too-old", start.Add(-time.Second), true, true, false},
		{"not-ended", end.Add(time.Hour), true, true, false},
		{"unpublished-results", end.AddDate(0, 0, -1), false, true, false},
		{"private-gallery", end.AddDate(0, 0, -1), true, false, false},
	} {
		confID, tag := insertSmokeConference(t, app)
		testTags[tag] = true
		exec(`UPDATE conferences SET publication_status='published', end_date=$2 WHERE id=$1`, confID, tc.ended)
		competitionID := createSmokeCompetition(t, app, CompetitionInput{ConferenceID: confID, Title: tc.name, Visibility: CompetitionVisibilityPublic})
		// Finalization predates the lookback: selection must use event end, not this timestamp.
		exec(`UPDATE competitions SET public_gallery_enabled=$2, results_finalized_at=CASE WHEN $3 THEN $4::timestamptz ELSE NULL END WHERE id=$1`, competitionID, tc.gallery, tc.finalized, start.AddDate(0, 0, -1))
		for rank := 1; rank <= 4; rank++ {
			person := insertSmokePerson(t, app, "weekly-winner")
			project := createSmokeProject(t, app, ProjectInput{CompetitionID: competitionID, CreatedByPersonID: person, Slug: fmt.Sprintf("weekly-%d-%s", rank, postgresSmokeSuffix()), Title: fmt.Sprintf("%s place %d", tc.name, rank)})
			exec(`UPDATE projects SET status='submitted' WHERE id=$1`, project)
			award, err := CreateAward(app, AwardInput{CompetitionID: competitionID, Title: fmt.Sprintf("Place %d", rank), AwardRank: &rank, Status: AwardStatusAvailable})
			if err != nil {
				t.Fatal(err)
			}
			// Fixture setup intentionally bypasses finalized-result editing restrictions.
			exec(`INSERT INTO project_awards(project_id,award_id) VALUES($1,$2)`, project, award)
			if tc.want && rank <= 3 {
				wanted[project] = true
			}
		}
	}
	got, err := weeklyNewsletterHackathonWinners(app, start, end)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range got {
		if !testTags[item.ConfTag] {
			continue
		}
		if !wanted[item.ProjectID] {
			t.Errorf("unexpected winner: %+v", item)
		}
		delete(wanted, item.ProjectID)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing top-three projects from qualifying events: %v", wanted)
	}
}
