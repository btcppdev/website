package handlers

import (
	"btcpp-web/external/xstudio"
	"btcpp-web/internal/config"
	"context"
	"net/url"
	"testing"
	"time"
)

func TestReportPostAttribution(t *testing.T) {
	start := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	p := sponsorReportPage{Event: reportEvent{Tag: "nairobi", Location: "Nairobi", Start: &start}, Saved: map[string]string{}, Sessions: []reportSession{{Title: "Building better payment systems", X: "https://x.com/i/broadcasts/abc"}}, PostLibrary: reportPostLibrary{Collected: "2026-10-23T12:00:00Z", Posts: []xstudio.AnalyticsPost{
		{ID: "1", Text: "Watch this", Links: []string{"https://btcpp.dev/nairobi/agenda"}, Impressions: 1000},
		{ID: "2", Text: "Later replay", Links: []string{"https://x.com/i/broadcasts/abc"}, Impressions: 2000},
		{ID: "3", Text: "Building better payment systems", Impressions: 3000},
		{ID: "4", Text: "See you in Nairobi", Impressions: 4000},
		{ID: "5", Text: "Unrelated announcement", Created: "2026-06-17T10:00:00Z", Impressions: 9000},
		{ID: "6", Text: "Other event", Links: []string{"https://btcpp.dev/nairobi27", "https://evil.test/nairobi"}, Impressions: 9000},
	}}}
	for _, r := range p.PostReview() {
		if r.Suggested != (r.ID < "5") {
			t.Fatalf("bad match %s: %s", r.ID, r.Reason)
		}
	}
	if len(p.ApprovedPosts()) != 0 {
		t.Fatal("suggestions counted without approval")
	}
	form := url.Values{"x_posts_editor": {"yes"}, "action": {"approve_x_posts"}, "x_post_2": {"exclude"}, "x_post_5": {"include"}}
	saved := map[string]string{}
	if err := saveReportPostChoices(form, saved, &p); err != nil {
		t.Fatal(err)
	}
	p.Saved = saved
	if got := postImpressions(p.ApprovedPosts()); got != 17000 {
		t.Fatalf("wrong approved total %d", got)
	}
	if p.Saved["x_post_2"] != "exclude" {
		t.Fatal("bulk approval overwrote exclusion")
	}
	p.Saved["x_post_999"] = "include"
	saved = map[string]string{}
	if err := saveReportPostChoices(url.Values{}, saved, &p); err != nil {
		t.Fatal(err)
	}
	if saved["x_post_999"] != "include" {
		t.Fatal("unavailable post choice lost")
	}
	form.Set("x_post_1", "invalid")
	if saveReportPostChoices(form, saved, &p) == nil {
		t.Fatal("invalid decision accepted")
	}
}
func TestReportPostAnnualDeduplication(t *testing.T) {
	old := reportPost{AnalyticsPost: xstudio.AnalyticsPost{ID: "1", Impressions: 5000}, Collected: "2026-10-22T00:00:00Z"}
	latest := old
	latest.Collected = "2026-10-23T00:00:00Z"
	latest.Impressions = 3000
	p := annualReportPage{Saved: map[string]string{}, Events: []annualEvent{{Included: true, Posts: []reportPost{old}}, {Included: true, Posts: []reportPost{latest}}, {Included: false, Posts: []reportPost{{AnalyticsPost: xstudio.AnalyticsPost{ID: "2", Impressions: 9000}}}}}}
	if postImpressions(p.ApprovedPosts()) != 3000 {
		t.Fatal("annual scope or newest-snapshot deduplication failed")
	}
	if len(p.HeadlinePlatforms()) != 1 {
		t.Fatal("missing approved annual impressions")
	}
	p.Saved = map[string]string{"reach_x_impressions": "10000", "reach_x_source": "Account export", "reach_x_period": "2026", "reach_x_date": "2026-10-23", "reach_x_coverage": "All account posts"}
	platforms := p.HeadlinePlatforms()
	if len(platforms) != 1 || platforms[0].HeadlineMetrics()[0].Value != "10000" {
		t.Fatal("account total combined with event total")
	}
	event := sponsorReportPage{Saved: map[string]string{"x_post_1": "include", "reach_x_impressions": "90000", "reach_x_source": "manual", "reach_x_period": "2026", "reach_x_date": "2026-10-23", "reach_x_coverage": "manual"}, PostLibrary: reportPostLibrary{Collected: latest.Collected, Posts: []xstudio.AnalyticsPost{latest.AnalyticsPost}}}
	totals := event.HeadlinePlatforms()
	if len(totals) != 1 || totals[0].HeadlineMetrics()[0].Value != "3000" {
		t.Fatal("approved posts did not replace manual impressions")
	}
}
func TestReportPostSnapshot(t *testing.T) {
	snapshot := defaultReportPosts(2026)
	if snapshot.Account != "btcplusplus" || len(snapshot.Posts) != 774 {
		t.Fatal("invalid bundled public snapshot")
	}
	var total int64
	for _, p := range snapshot.Posts {
		total += p.Impressions
	}
	if total != 1160550 {
		t.Fatalf("invalid snapshot sum %d", total)
	}
	if len(defaultReportPosts(2027).Posts) != 0 {
		t.Fatal("2026 posts leaked into another year")
	}
}

func TestReportPostSnapshotPersistencePostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	app := &config.AppContext{DB: db}
	_, err := db.Exec(ctx, `CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := loadReportPosts(ctx, db, 2026)
	if err != nil || len(initial.Posts) != 774 {
		t.Fatal("bundled fallback unavailable", err)
	}
	fresh := reportPostLibrary{Account: "btcplusplus", Year: 2026, Collected: "2026-10-24T00:00:00Z", Posts: []xstudio.AnalyticsPost{{ID: "1", Impressions: 2000}}}
	if err = saveReportPostSnapshot(ctx, app, fresh); err != nil {
		t.Fatal(err)
	}
	stale := fresh
	stale.Collected = "2026-10-23T00:00:00Z"
	stale.Posts = nil
	if err = saveReportPostSnapshot(ctx, app, stale); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadReportPosts(ctx, db, 2026)
	if err != nil || len(loaded.Posts) != 1 || loaded.Collected != fresh.Collected {
		t.Fatal("stale refresh overwrote snapshot", err)
	}
	other, err := loadReportPosts(ctx, db, 2027)
	if err != nil || len(other.Posts) != 0 {
		t.Fatal("snapshot leaked into another year", err)
	}
}
