package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"html/template"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: this harness can only seed its named, loopback demo database.
// It uses the report-query test schema, not a replacement application database.
func TestReportDemo(t *testing.T) {
	if os.Getenv("REPORT_DEMO") != "1" {
		t.Skip("local synthetic report demo")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(os.Getenv("REPORT_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Database != "btcpp_report_demo" || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("requires loopback btcpp_report_demo database")
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["search_path"] = "report_demo"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`DROP SCHEMA IF EXISTS report_demo CASCADE; CREATE SCHEMA report_demo`)
	ddl := strings.Split(reportFixtureSQL, "INSERT INTO registrations")[0]
	exec(strings.ReplaceAll(ddl, "CREATE TEMP TABLE", "CREATE TABLE"))
	exec(`CREATE TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now());
 INSERT INTO registrations SELECT 'event',false,CASE WHEN n<=286 THEN '2026-06-17'::timestamptz END,'attendee-'||n,'demo-'||n||'@example.test',NULL FROM generate_series(1,320) n;
 INSERT INTO competitions VALUES('competition','event','2026-06-19');
 INSERT INTO projects SELECT 'project-'||n,'competition','submitted',CASE n WHEN 1 THEN 'Demo: Lightning Bridge' WHEN 2 THEN 'Demo: Contributor Compass' WHEN 3 THEN 'Demo: Community Wallet' ELSE 'Demo project '||n END,'Synthetic prototype for report layout testing.','','','' FROM generate_series(1,32) n;
 INSERT INTO project_members SELECT 'project-'||n,'builder-'||row_number() OVER(ORDER BY n,m) FROM generate_series(1,32) n CROSS JOIN LATERAL generate_series(1,CASE WHEN n<=4 THEN 1 WHEN n<=12 THEN 2 WHEN n<=20 THEN 3 WHEN n<=28 THEN 4 ELSE 5 END) m;
 INSERT INTO awards(id,competition_id,award_type,sponsored_by_org_id,status,archived_at,title) SELECT 'award-'||n,'competition','challenge','demo-org','awarded',NULL,'Demo challenge '||n FROM generate_series(1,6) n;
 INSERT INTO project_awards SELECT 'project-'||((n-1)%3+1),'award-'||n FROM generate_series(1,6) n;
 INSERT INTO award_distributions VALUES('competition','project-1',2500000,'sats','sent'),('competition','project-2',1500000,'sats','claimed'),('competition','project-3',1000000,'sats','sent');
 INSERT INTO people SELECT 'builder-'||n,'Demo builder '||n,'','' FROM generate_series(1,96) n;
 INSERT INTO volunteers SELECT 'volunteer-'||n,'crew-'||n FROM generate_series(1,18) n;
 INSERT INTO work_shifts VALUES('shift','event');
 INSERT INTO work_shifts_volunteers SELECT 'shift','volunteer-'||n FROM generate_series(1,18) n;`)
	fixture := reportFixturePage(t)
	saved := map[string]string{"program_reviewed": "yes", "applications": "84", "applications_source": "Synthetic CFP fixture — not a real intake export", "analytics_metric": "YouTube views", "analytics_source": "Synthetic fixture — not a YouTube export", "analytics_period": "Illustrative lifetime totals", "analytics_date": "2026-09-20"}
	for i, p := range fixture.Speakers {
		exec(`INSERT INTO people VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, p.ID, p.Name, p.Photo, p.Company)
		exec(`INSERT INTO speaker_confs(id,speaker_id) VALUES($1,$2)`, fmt.Sprint("speaker-", i), p.ID)
		saved["speaker_"+p.ID] = "yes"
	}
	for _, p := range fixture.Judges {
		exec(`INSERT INTO people VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, p.ID, p.Name, p.Photo, p.Company)
		exec(`INSERT INTO competition_judges VALUES('competition',$1,'','Demo judge roster')`, p.ID)
	}
	titles := []string{"Demo: Bitcoin Core contribution pathways", "Demo: Building with Lightning", "Demo: Wallet security in practice", "Demo: Open-source funding models", "Demo: Testing Bitcoin applications"}
	for i := 1; i <= 35; i++ {
		id := fmt.Sprint("session-", i)
		kind := "talk"
		if i > 24 {
			kind = "workshop"
		}
		if i > 32 {
			kind = "panel"
		}
		title := fmt.Sprintf("Demo %s %02d", kind, i)
		if i <= len(titles) {
			title = titles[i-1]
		}
		exec(`INSERT INTO proposals VALUES($1,'event',$2,$3,'Scheduled')`, id, title, kind)
		exec(`INSERT INTO conf_talks(id,conference_id,proposal_id,scheduled_start,scheduled_end,archived_at) VALUES($1,'event',$1,'2026-06-17T10:00:00+03','2026-06-17T10:30:00+03',NULL)`, id)
		exec(`INSERT INTO proposals_speaker_confs VALUES($1,$2)`, id, fmt.Sprint("speaker-", (i-1)%len(fixture.Speakers)))
		saved["delivered_"+id] = "yes"
		if i <= 5 {
			saved["views_"+id] = fmt.Sprint([]int{12400, 8700, 6300, 4100, 2800}[i-1])
		}
		if kind == "panel" {
			saved["panel_"+id] = "yes"
			saved["session_note_"+id] = "Illustrative panel recap. Replace with verified discussion takeaways."
		}
	}
	saved["social"] = "ILLUSTRATIVE ACCOUNT SNAPSHOT — all figures below are invented.\n\nYouTube: 62,400 views · 4,180 watch hours · 310 net new subscribers.\n\nX: 840,000 impressions · 28,600 engagements · 920 net new followers.\n\nExample reporting period: 1 January–19 September 2026. Account-wide totals are separate from Nairobi recording views; they are not event-attributed reach. No analytics export was used."
	raw, _ := json.Marshal(saved)
	exec(`INSERT INTO sponsor_report_drafts(scope,content) VALUES('event-v2/event',$1)`, raw)
	load := func() (sponsorReportPage, error) {
		p := sponsorReportPage{Synthetic: true, Event: fixture.Event, Generated: time.Now().UTC().Format(time.RFC3339) + " · SYNTHETIC DEMO", Message: "Synthetic database-backed demo. All counts, views and payouts are invented. Public portraits are retained only to test layout."}
		p.Event.ID = "event"
		p.Sections = eventReportSections(p.Event)
		p.Options = []reportEvent{p.Event}
		p.Sections = append(p.Sections[:1], append([]reportSection{{Key: "social", Title: "Beyond the room · demo analytics"}}, p.Sections[1:]...)...)
		var data []byte
		if e := db.QueryRow(ctx, `SELECT content FROM sponsor_report_drafts WHERE scope='event-v2/event'`).Scan(&data); e != nil {
			return p, e
		}
		if e := json.Unmarshal(data, &p.Saved); e != nil {
			return p, e
		}
		if e := loadEventReport(ctx, &config.AppContext{DB: db}, &p); e != nil {
			return p, e
		}
		hydrateEventReport(&p)
		return p, nil
	}
	p, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if p.Attendees != 286 || p.Teams != 32 || p.Participants != 96 || p.Sats != 5000000 || p.Talks != 24 || p.Workshops != 8 || p.PanelCount != 3 || p.Volunteers != 18 || len(p.Popular) != 5 || len(p.Winners) != 3 {
		t.Fatalf("unexpected demo aggregates: %+v", p.Metrics)
	}
	t.Log("Verified synthetic SQL aggregates: 286 attendees, 32 teams, 96 hackers, 5M sats, 24 talks, 8 workshops, 3 panels")
	if os.Getenv("REPORT_PREVIEW") != "1" {
		return
	}
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/reports/preview", renderReportMarkdownPreview)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../../static"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Demo is read-only; edit the local fixture to change data", 405)
			return
		}
		p, e := load()
		if e != nil {
			http.Error(w, "Demo load failed", 500)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if e = tmpl.Execute(w, p); e != nil {
			t.Log(e)
		}
	})
	t.Log("Synthetic report: http://127.0.0.1:8106")
	t.Fatal(http.ListenAndServe("127.0.0.1:8106", mux))
}
