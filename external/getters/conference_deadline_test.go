package getters

import (
	"testing"
	"time"
)

func TestDatabaseSmokeSpeakerDeadlineRoundTrip(t *testing.T) {
	app := databaseSmokeContext(t)
	id, tag := insertSmokeConference(t, app)
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	closeAt := time.Date(2026, 9, 26, 0, 0, 0, 0, loc)
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 2)
	in := ConfDetailsInput{Description: "Deadline test", StartDate: &start, EndDate: &end, Timezone: "Asia/Seoul", AccentColor: "#f9af5e", SpeakerApplicationsClose: &closeAt}
	if err := UpdateConfDetails(app, id, in); err != nil {
		t.Fatal(err)
	}
	got, err := GetConfByTag(app, tag)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpeakerApplicationsClose == nil || !got.TalksDueDate().Equal(closeAt) {
		t.Fatalf("deadline not persisted: %+v", got)
	}
	if got.TalksDueLabel() != "Sat. Sep 26, 2026 at 12:00 AM KST (UTC+09:00)" {
		t.Fatal(got.TalksDueLabel())
	}
	in.SpeakerApplicationsClose = nil
	if err := UpdateConfDetails(app, id, in); err != nil {
		t.Fatal(err)
	}
	got, err = GetConfByTag(app, tag)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpeakerApplicationsClose != nil || !got.TalksDueDate().Equal(start.AddDate(0, 0, -45)) {
		t.Fatal("cleared deadline did not restore default")
	}
}
