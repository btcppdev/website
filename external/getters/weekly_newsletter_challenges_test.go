package getters

import (
	"context"
	"testing"
	"time"
)

func TestWeeklyNewsletterSponsorChallenges(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	confID, tag := insertSmokeConference(t, app)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE conferences SET publication_status='published' WHERE id=$1`, confID)
	var orgID, competitionID string
	if err := app.DB.QueryRow(ctx, `INSERT INTO organizations(name) VALUES('Newsletter sponsor') RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = app.DB.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID) })
	if err := app.DB.QueryRow(ctx, `INSERT INTO competitions(conference_id,title,visibility) VALUES($1,'Newsletter Hackathon','public') RETURNING id::text`, confID).Scan(&competitionID); err != nil {
		t.Fatal(err)
	}
	insert := func(title, status, kind string, sponsor any) string {
		t.Helper()
		var id string
		if err := app.DB.QueryRow(ctx, `INSERT INTO awards(competition_id,sponsored_by_org_id,title,status,award_type,created_at) VALUES($1,$2,$3,$4,$5,now()-interval '30 days') RETURNING id::text`, competitionID, sponsor, title, status, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := insert("New sponsor challenge", "draft", "challenge", orgID)
	var before *time.Time
	if err := app.DB.QueryRow(ctx, `SELECT published_at FROM awards WHERE id=$1`, id).Scan(&before); err != nil || before != nil {
		t.Fatalf("draft has publication timestamp: %v %v", before, err)
	}
	exec(`UPDATE awards SET status='available' WHERE id=$1`, id)
	var published time.Time
	if err := app.DB.QueryRow(ctx, `SELECT published_at FROM awards WHERE id=$1`, id).Scan(&published); err != nil {
		t.Fatal(err)
	}
	insert("Unapproved draft", "draft", "challenge", orgID)
	insert("Normal prize", "available", "normal", orgID)
	insert("Unsponsored challenge", "available", "challenge", nil)
	archived := insert("Archived challenge", "available", "challenge", orgID)
	exec(`UPDATE awards SET archived_at=now() WHERE id=$1`, archived)
	check := func(start, end time.Time, want int) {
		t.Helper()
		items, err := weeklyNewsletterSponsorChallenges(app, start, end)
		if err != nil {
			t.Fatal(err)
		}
		var matched []WeeklyNewsletterChallenge
		for _, item := range items {
			if item.ConfTag == tag {
				matched = append(matched, item)
			}
		}
		if len(matched) != want {
			t.Fatalf("got %d challenges, want %d: %+v", len(matched), want, matched)
		}
		if want == 1 && (matched[0].AwardID != id || matched[0].SponsorName != "Newsletter sponsor" || matched[0].PublicSlug == "") {
			t.Fatalf("wrong challenge details: %+v", matched)
		}
	}
	check(published, published.Add(time.Hour), 1)
	check(published.Add(-time.Hour), published, 0)
	check(published.Add(time.Microsecond), published.Add(time.Hour), 0)
	// Edits and re-publication must not make an old award a new announcement.
	exec(`UPDATE awards SET title='Edited challenge' WHERE id=$1`, id)
	exec(`UPDATE awards SET status='draft' WHERE id=$1`, id)
	check(published, published.Add(time.Hour), 0)
	exec(`UPDATE awards SET status='available' WHERE id=$1`, id)
	var again time.Time
	if err := app.DB.QueryRow(ctx, `SELECT published_at FROM awards WHERE id=$1`, id).Scan(&again); err != nil || !again.Equal(published) {
		t.Fatalf("publication date changed: %v %v", again, err)
	}
	check(published, published.Add(time.Hour), 1)
	exec(`UPDATE competitions SET visibility='hidden' WHERE id=$1`, competitionID)
	check(published, published.Add(time.Hour), 0)
	exec(`UPDATE competitions SET visibility='public' WHERE id=$1`, competitionID)
	exec(`UPDATE conferences SET publication_status='draft' WHERE id=$1`, confID)
	check(published, published.Add(time.Hour), 0)
}
