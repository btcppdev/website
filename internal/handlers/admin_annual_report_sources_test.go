package handlers

import (
	"btcpp-web/external/xstudio"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAnnualReportExclusionsPersistAndNewEventsEnter(t *testing.T) {
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	old := now.AddDate(0, -1, 0)
	newEvent := reportEvent{ID: "00000000-0000-4000-8000-000000000001", End: &now}
	oldEvent := reportEvent{ID: "00000000-0000-4000-8000-000000000002", End: &old}
	p := annualReportPage{Year: 2026, DraftUpdatedAt: &old, Saved: map[string]string{"selection_saved": "yes"}}
	if !annualEventIncluded(p, newEvent) || annualEventIncluded(p, oldEvent) {
		t.Fatal("legacy selection lost exclusions or failed to pick up a newly completed event")
	}
	form := url.Values{"action": {"save"}, "cutoff": {"2026-11-11"}, "event_selection_mode": {"exclude"}, "event_seen": {newEvent.ID, oldEvent.ID}, "event": {newEvent.ID}, "source_seen": {"exclude_post_123"}, "exclude_post_123": {"yes"}}
	if err := annualReportForm(&p, form, now); err != nil {
		t.Fatal(err)
	}
	if annualEventIncluded(p, oldEvent) || !annualEventIncluded(p, newEvent) || p.Saved["exclude_post_123"] != "yes" {
		t.Fatal("explicit exclusions were not saved")
	}
	if !annualEventIncluded(p, reportEvent{ID: "newly-added"}) {
		t.Fatal("new event required approval")
	}
	form.Del("source_seen")
	if err := annualReportForm(&p, form, now); err != nil || p.Saved["exclude_post_123"] != "yes" {
		t.Fatal("temporarily unavailable source lost its exclusion", err)
	}
	form.Set("action", "include_all")
	if err := annualReportForm(&p, form, now); err != nil || !annualEventIncluded(p, oldEvent) || p.Saved["exclude_post_123"] != "yes" {
		t.Fatal("restoring events also changed source exclusions", err)
	}
	form.Set("source_seen", "exclude_post_123")
	form.Del("exclude_post_123")
	if err := annualReportForm(&p, form, now); err != nil || p.Saved["exclude_post_123"] != "" {
		t.Fatal("source could not be restored", err)
	}
}

func TestAnnualReportSourceExclusions(t *testing.T) {
	media := reportMedia{Platform: "YouTube", ID: "abcdefghijk", Views: 10000}
	post := reportPost{AnalyticsPost: xstudio.AnalyticsPost{ID: "123", Impressions: 20000}}
	p := annualReportPage{Saved: map[string]string{media.ExclusionKey(): "yes", post.ExclusionKey(): "yes"}, Events: []annualEvent{{Included: true, Media: []reportMedia{media}, Posts: []reportPost{post}}}}
	if len(p.AllMedia()) != 0 || len(p.ApprovedPosts()) != 0 || len(p.MediaTotals()) != 0 || len(p.HeadlinePlatforms()) != 0 {
		t.Fatal("excluded sources still contribute to report")
	}
	if choices := p.SourceChoices(); len(choices) != 2 || !choices[0].Excluded || !choices[1].Excluded {
		t.Fatal("excluded sources disappeared from review controls", choices)
	}
}

func TestReportSourceFreshness(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		age    time.Duration
		recent bool
	}{{0, true}, {5 * time.Hour, true}, {6 * time.Hour, false}, {-time.Minute, false}} {
		if reportSourceRecent(now.Add(-tt.age).Format(time.RFC3339), now) != tt.recent {
			t.Fatal("wrong freshness", tt.age)
		}
	}
	if reportSourceRecent("invalid", now) {
		t.Fatal("invalid collection date treated as fresh")
	}
}

func TestAnnualReportOfferedPrizesPostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, reportFixtureSQL+`
 UPDATE competitions SET results_finalized_at=NULL;
 DELETE FROM award_distributions;
 INSERT INTO project_awards VALUES ('project1','award1'),('project2','award1');
 INSERT INTO competitions VALUES ('other','other-event',NULL);
 INSERT INTO awards(id,competition_id,status,archived_at,max_awardees) VALUES
 ('available','competition','available',NULL,2),('unawarded','competition','unawarded',NULL,1),
 ('archived','competition','available',now(),1),('foreign','other','available',NULL,1);
 INSERT INTO prizes VALUES ('available','available','sats','1000000'),('unawarded','unawarded','sats','500000'),
 ('archived','archived','sats','9999999'),('foreign','foreign','sats','9999999');`)
	if err != nil {
		t.Fatal(err)
	}
	var sats int64
	if err = db.QueryRow(ctx, `SELECT coalesce(sum(sats),0)::bigint FROM (`+reportOfferedPrizesSQL+`) offered`, "event").Scan(&sats); err != nil {
		t.Fatal(err)
	}
	if sats != 2503000 {
		t.Fatalf("offered total depends on winners or payouts, or includes invalid prizes: %d", sats)
	}
}

func TestAnnualReportRefreshCachesAndPreservesEditsPostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, reportFixtureSQL+`
 CREATE TEMP TABLE conferences(id text,tag text,description text,location text,timezone text,date_desc text,start_date timestamptz,end_date timestamptz,publication_status text);
 CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now());
 INSERT INTO conferences VALUES ('event','example','Example','Example','UTC','','2026-01-01','2026-01-02','published');`)
	if err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{DB: db, Env: &types.EnvConfig{}}
	previous := []reportMedia{{Platform: "X", ID: "abc", URL: "https://x.com/i/broadcasts/abc", Views: 12000, Collected: "2026-01-03T00:00:00Z"}}
	raw, _ := json.Marshal(previous)
	saved := map[string]string{"note": "Keep my event narrative", "x_post_123": "exclude", "media_snapshot": string(raw)}
	if err = saveEventReportDraft(ctx, app, "event-v2/event", 0, saved); err != nil {
		t.Fatal(err)
	}
	refresh := func(action, automatic string) *httptest.ResponseRecorder {
		form := url.Values{"action": {action}, "automatic": {automatic}, "event_id": {"event"}}
		r := httptest.NewRequest("POST", "/admin/reports/annual", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.ParseForm()
		w := httptest.NewRecorder()
		p := annualReportPage{Year: 2026, Cutoff: "2026-01-03", Saved: map[string]string{}}
		refreshAnnualSource(ctx, w, r, app, &p)
		return w
	}
	if w := refresh("refresh_annual_event", "yes"); w.Code != 200 || !strings.Contains(w.Body.String(), "previous snapshot retained") {
		t.Fatalf("refresh failure not surfaced: %d %s", w.Code, w.Body.String())
	}
	if w := refresh("refresh_annual_event", "yes"); w.Code != 200 || !strings.Contains(w.Body.String(), "Last checked") {
		t.Fatalf("automatic refresh ignored cooldown: %d %s", w.Code, w.Body.String())
	}
	var revision int64
	if err = db.QueryRow(ctx, `SELECT content,revision FROM sponsor_report_drafts WHERE scope='event-v2/event'`).Scan(&raw, &revision); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if revision != 2 || saved["note"] != "Keep my event narrative" || saved["x_post_123"] != "exclude" || !strings.Contains(saved["media_snapshot"], "12000") {
		t.Fatal("automatic collection overwrote edits, lost imported recordings, or refreshed twice", revision, saved)
	}
	posts := reportPostLibrary{Account: "btcplusplus", Year: 2026, Collected: time.Now().UTC().Format(time.RFC3339), Posts: []xstudio.AnalyticsPost{{ID: "123", Text: "Example", Impressions: 5000}}}
	if err = saveReportPostSnapshot(ctx, app, posts); err != nil {
		t.Fatal(err)
	}
	if w := refresh("refresh_annual_posts", "yes"); w.Code != 200 || !strings.Contains(w.Body.String(), "Using X post analytics") {
		t.Fatalf("fresh posts unnecessarily fetched: %d %s", w.Code, w.Body.String())
	}
	if w := refresh("refresh_annual_posts", "no"); w.Code != 502 {
		t.Fatal("manual retry did not attempt upstream collection", w.Code)
	}
	loaded, err := loadReportPosts(ctx, db, 2026)
	if err != nil || loaded.Collected != posts.Collected || len(loaded.Posts) != 1 {
		t.Fatal("failed post refresh lost previous data", err)
	}
}
