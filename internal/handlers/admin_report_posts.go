package handlers

import (
	"btcpp-web/external/xstudio"
	"btcpp-web/internal/config"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Public post content and a dated metrics snapshot; no session credentials.
//
//go:embed reportdata/x-posts-2026.json
var reportPosts2026 []byte

type reportPostLibrary = xstudio.PostAnalytics

type reportPost struct {
	xstudio.AnalyticsPost
	Collected string
}

func (p reportPost) URL() string     { return "https://x.com/btcplusplus/status/" + p.ID }
func (p reportPost) Display() string { return compactReportSats(p.Impressions) }

type reportPostReview struct {
	reportPost
	Choice, Reason string
	Suggested      bool
}

func reportPostYear(e reportEvent) int {
	if e.Start != nil {
		return e.Start.In(reportEventLocation(e)).Year()
	}
	return time.Now().Year()
}
func defaultReportPosts(year int) xstudio.PostAnalytics {
	var data xstudio.PostAnalytics
	if year == 2026 {
		_ = json.Unmarshal(reportPosts2026, &data)
	}
	return data
}

type reportPostQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadReportPosts(ctx context.Context, db reportPostQuerier, year int) (xstudio.PostAnalytics, error) {
	data := defaultReportPosts(year)
	var raw []byte
	err := db.QueryRow(ctx, `SELECT content FROM sponsor_report_drafts WHERE scope=$1`, fmt.Sprintf("x-posts/%d", year)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return data, nil
	}
	if err != nil {
		return data, err
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return data, err
	}
	if data.Year != year || !strings.EqualFold(data.Account, "btcplusplus") {
		return data, fmt.Errorf("unexpected X analytics account or year")
	}
	return data, nil
}
func refreshReportPosts(ctx context.Context, app *config.AppContext, year int) error {
	client, err := xstudio.New(xstudio.Config{Cookie: app.Env.Recordings.X.Cookie, UserAgent: app.Env.Recordings.X.UserAgent, IngestID: app.Env.Recordings.X.IngestID})
	if err != nil {
		return err
	}
	data, err := client.PostAnalytics(ctx, year, time.Now().UTC())
	if err != nil {
		return err
	}
	if !strings.EqualFold(data.Account, "btcplusplus") {
		return fmt.Errorf("X session belongs to a different account")
	}
	return saveReportPostSnapshot(ctx, app, data)
}
func saveReportPostSnapshot(ctx context.Context, app *config.AppContext, data reportPostLibrary) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// Keep the newer snapshot if two administrators refresh concurrently.
	_, err = app.DB.Exec(ctx, `INSERT INTO sponsor_report_drafts(scope,content,revision) VALUES($1,$2::jsonb,1)
 ON CONFLICT(scope) DO UPDATE SET content=excluded.content,revision=sponsor_report_drafts.revision+1,updated_at=now()
 WHERE (sponsor_report_drafts.content->>'collected')::timestamptz < (excluded.content->>'collected')::timestamptz`, fmt.Sprintf("x-posts/%d", data.Year), raw)
	return err
}

var reportPostWords = regexp.MustCompile(`[^\pL\pN]+`)

func normalizePostText(s string) string {
	return strings.TrimSpace(reportPostWords.ReplaceAllString(strings.ToLower(s), " "))
}
func containsPostPhrase(text, phrase string) bool {
	phrase = normalizePostText(phrase)
	return len(phrase) >= 4 && strings.Contains(" "+normalizePostText(text)+" ", " "+phrase+" ")
}
func (p sponsorReportPage) postMatcher() func(xstudio.AnalyticsPost) (bool, string) {
	links := map[string]bool{}
	for _, s := range p.Sessions {
		for _, raw := range []string{s.X, s.YouTube} {
			platform, id := reportMediaIdentity(raw)
			if platform != "" {
				links[platform+":"+id] = true
			}
		}
	}
	for _, m := range p.Media() {
		links[m.Platform+":"+m.ID] = true
	}
	for _, line := range strings.Split(p.Saved["media_links"], "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			platform, id := reportMediaIdentity(f[0])
			if platform != "" {
				links[platform+":"+id] = true
			}
		}
	}
	return func(post xstudio.AnalyticsPost) (bool, string) {
		for _, raw := range post.Links {
			u, err := url.Parse(raw)
			if err != nil {
				continue
			}
			if (u.Hostname() == "btcpp.dev" || u.Hostname() == "www.btcpp.dev") && (u.Path == "/"+p.Event.Tag || strings.HasPrefix(u.Path, "/"+p.Event.Tag+"/")) {
				return true, "Links to this event"
			}
			platform, id := reportMediaIdentity(raw)
			if platform != "" && links[platform+":"+id] {
				return true, "Links to a matched recording"
			}
		}
		for _, s := range p.Sessions {
			if len(normalizePostText(s.Title)) >= 20 && containsPostPhrase(post.Text, s.Title) {
				return true, "Mentions a program title: " + s.Title
			}
		}
		if containsPostPhrase(post.Text, p.Event.Tag) || containsPostPhrase(post.Text, p.Event.Location) {
			return true, "Mentions the event location or tag — confirm the edition"
		}
		return false, "No clear match — review manually or leave annual-only"
	}
}
func (p sponsorReportPage) PostReview() []reportPostReview {
	var rows []reportPostReview
	matcher := p.postMatcher()
	for _, post := range p.PostLibrary.Posts {
		match, reason := matcher(post)
		rows = append(rows, reportPostReview{reportPost: reportPost{AnalyticsPost: post, Collected: p.PostLibrary.Collected}, Choice: p.Saved["x_post_"+post.ID], Reason: reason, Suggested: match})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Suggested != rows[j].Suggested {
			return rows[i].Suggested
		}
		return rows[i].Impressions > rows[j].Impressions
	})
	return rows
}
func uniqueReportPosts(posts []reportPost) []reportPost {
	byID := map[string]reportPost{}
	for _, p := range posts {
		old, ok := byID[p.ID]
		t, _ := time.Parse(time.RFC3339Nano, p.Collected)
		ot, _ := time.Parse(time.RFC3339Nano, old.Collected)
		if !ok || t.After(ot) {
			byID[p.ID] = p
		}
	}
	var out []reportPost
	for _, p := range byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Impressions != out[j].Impressions {
			return out[i].Impressions > out[j].Impressions
		}
		return out[i].ID < out[j].ID
	})
	return out
}
func (p sponsorReportPage) ApprovedPosts() []reportPost {
	var out []reportPost
	for _, post := range p.PostLibrary.Posts {
		if p.Saved["x_post_"+post.ID] == "include" {
			out = append(out, reportPost{AnalyticsPost: post, Collected: p.PostLibrary.Collected})
		}
	}
	return uniqueReportPosts(out)
}
func (p annualReportPage) ApprovedPosts() []reportPost {
	var out []reportPost
	for _, e := range p.Included() {
		out = append(out, e.Posts...)
	}
	return uniqueReportPosts(out)
}
func postImpressions(posts []reportPost) int64 {
	var n int64
	for _, p := range uniqueReportPosts(posts) {
		n += p.Impressions
	}
	return n
}
func postHighlights(posts []reportPost) []reportPost {
	posts = uniqueReportPosts(posts)
	if len(posts) > 3 {
		posts = posts[:3]
	}
	return posts
}
func (p sponsorReportPage) PostHighlights() []reportPost { return postHighlights(p.ApprovedPosts()) }
func (p annualReportPage) PostHighlights() []reportPost  { return postHighlights(p.ApprovedPosts()) }
func saveReportPostChoices(form url.Values, saved map[string]string, p *sponsorReportPage) error {
	// Preserve decisions for older/unavailable posts as well as forms without this editor.
	for k, v := range p.Saved {
		if strings.HasPrefix(k, "x_post_") {
			saved[k] = v
		}
	}
	if form.Get("x_posts_editor") != "yes" {
		return nil
	}
	for _, row := range p.PostReview() {
		key := "x_post_" + row.ID
		choice := form.Get(key)
		if choice != "" && choice != "include" && choice != "exclude" {
			return fmt.Errorf("invalid X post attribution")
		}
		if form.Get("action") == "approve_x_posts" && row.Suggested && choice == "" {
			choice = "include"
		}
		saved[key] = choice
	}
	return nil
}

func reportPostsPlatform(posts []reportPost) reportReachPlatform {
	date := ""
	for _, p := range posts {
		if p.Collected > date {
			date = p.Collected
		}
	}
	if len(date) >= 10 {
		date = date[:10]
	}
	return reportReachPlatform{Key: "x_posts", Name: "X", Source: "@btcplusplus · X account analytics", Date: date, Period: "Lifetime counters through collection", Coverage: fmt.Sprintf("%d approved event posts", len(uniqueReportPosts(posts))), Metrics: []reportReachMetric{{Key: "reach_x_impressions", Label: "post impressions", Value: strconv.FormatInt(postImpressions(posts), 10)}}}
}
func withApprovedPostPlatform(platforms []reportReachPlatform, posts []reportPost) []reportReachPlatform {
	if len(posts) == 0 {
		return platforms
	}
	for i := range platforms {
		if platforms[i].Key != "x" {
			continue
		}
		var metrics []reportReachMetric
		for _, m := range platforms[i].Metrics {
			if m.Key != "reach_x_impressions" {
				metrics = append(metrics, m)
			}
		}
		platforms[i].Metrics = metrics
	}
	return append(platforms, reportPostsPlatform(posts))
}

func (p reportPost) Excerpt() string {
	text := []rune(p.Text)
	if len(text) > 180 {
		return string(text[:180]) + "…"
	}
	return p.Text
}
func (p annualReportPage) TopPost() *reportPost {
	posts := p.ApprovedPosts()
	if len(posts) == 0 {
		return nil
	}
	return &posts[0]
}
