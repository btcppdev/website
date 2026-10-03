package handlers

import (
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReportAnonymousDenied(t *testing.T) {
	session := scs.New()
	ctx, err := session.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	AdminSponsorReportPreview(w, httptest.NewRequest("POST", "/admin/reports/preview", nil).WithContext(ctx), &config.AppContext{Session: session})
	if w.Code != 303 {
		t.Fatalf("anonymous preview: %d", w.Code)
	}
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		AdminSponsorReport(w, httptest.NewRequest(method, "/admin/reports", nil).WithContext(ctx), &config.AppContext{Session: session})
		if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/login?") {
			t.Fatalf("anonymous %s: %d", method, w.Code)
		}
	}
}
func reportFixturePage(t *testing.T) sponsorReportPage {
	t.Helper()
	raw, err := os.ReadFile("../../docs/nairobi-report-draft.json")
	if err != nil {
		t.Fatal(err)
	}
	var page sponsorReportPage
	if err = json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	page.Sections = eventReportSections(page.Event)
	page.Options = []reportEvent{page.Event}
	hydrateEventReport(&page)
	return page
}
func TestReportRender(t *testing.T) {
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	page := reportFixturePage(t)
	var out bytes.Buffer
	if err = tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Nairobi, Kenya", "WestToEast", "Judy Imasuen", "The program, delivered", "Conversations reaching further", "Backing the winning projects", "Save event draft", "Portrait of", "program_reviewed"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	page.Sections[0].Text = "</textarea><script>alert(1)</script>"
	out.Reset()
	if err = tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>alert") {
		t.Fatal("narrative is executable")
	}
	if os.Getenv("REPORT_PREVIEW") == "1" {
		page = reportFixturePage(t)
		mux := http.NewServeMux()
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../../static"))))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { tmpl.Execute(w, page) })
		addr := os.Getenv("REPORT_PREVIEW_ADDR")
		if addr == "" {
			addr = "127.0.0.1:8102"
		}
		mux.HandleFunc("/chart-demo", func(w http.ResponseWriter, r *http.Request) {
			demo := page
			demo.Preview = false
			demo.Message = "SYNTHETIC TEST DATA — chart layout check only, not event statistics"
			demo.ProgramReviewed = true
			demo.Talks, demo.Workshops, demo.PanelCount = 24, 8, 4
			demo.Saved = map[string]string{"analytics_metric": "YouTube views", "analytics_source": "SYNTHETIC TEST DATA", "analytics_period": "Layout check", "analytics_date": "2026-09-17"}
			views := int64(1500)
			demo.Popular = []reportSession{{Title: "Example technical session — synthetic data", Speakers: "Example speaker", Views: &views}}
			demo.Winners = []reportWinner{{Title: "Example project A", Sats: 1000000}, {Title: "Example project B", Sats: 500000}, {Title: "Example project C", Sats: 0}}
			tmpl.Execute(w, demo)
		})
		t.Log("Nairobi public-source draft: http://" + addr)
		t.Fatal(http.ListenAndServe(addr, mux))
	}
	out.Reset()
	if err = tmpl.Execute(&out, sponsorReportPage{Options: page.Options}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Save event draft") || !strings.Contains(out.String(), "Open event report") {
		t.Fatal("index rendered report editor")
	}
}
func TestReportCountsAndProvenance(t *testing.T) {
	page := reportFixturePage(t)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	form := url.Values{"applications": {"-1"}}
	if _, err := eventReportForm(form, &page, now); err == nil {
		t.Fatal("negative accepted")
	}
	form.Set("applications", "0")
	if _, err := eventReportForm(form, &page, now); err == nil {
		t.Fatal("unsourced count accepted")
	}
	form.Set("applications_source", "CFP export")
	form.Set("day_2026-06-17", "100")
	form.Set("day_2026-06-18", "80")
	form.Set("attendance_source", "Door reconciliation")
	form.Set("views_"+page.Sessions[0].ID, "500")
	if _, err := eventReportForm(form, &page, now); err == nil {
		t.Fatal("unsourced popularity accepted")
	}
	form.Set("analytics_source", "YouTube Studio export")
	form.Set("analytics_metric", "YouTube views")
	form.Set("analytics_period", "Lifetime to 2026-09-16")
	form.Set("analytics_date", "2026-09-16")
	form.Set("program_reviewed", "yes")
	form.Set("delivered_"+page.Sessions[0].ID, "yes")
	form.Set("speaker_foreign-event", "yes")
	saved, err := eventReportForm(form, &page, now)
	if err != nil {
		t.Fatal(err)
	}
	if saved["speaker_foreign-event"] != "" {
		t.Fatal("foreign selection persisted")
	}
	rebuilt := sponsorReportPage{Event: page.Event, Sections: eventReportSections(page.Event), Sessions: page.Sessions, Saved: saved, Attendees: 120}
	hydrateEventReport(&rebuilt)
	if rebuilt.AttendeeDays != 180 || rebuilt.ReportedDays != 2 || rebuilt.Attendees != 120 {
		t.Fatalf("attendance collapsed: %+v", rebuilt)
	}
	if rebuilt.Applications == nil || *rebuilt.Applications != 0 || rebuilt.PanelCount != 1 || len(rebuilt.Popular) != 1 {
		t.Fatal("zero/panel/ranking handling failed")
	}
	future := now.Add(time.Hour)
	page.Sessions[0].End = &future
	if _, err = eventReportForm(form, &page, now); err == nil {
		t.Fatal("future delivery accepted")
	}
}
func TestReportCalendarDays(t *testing.T) {
	start := time.Date(2026, 6, 16, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 19, 16, 0, 0, 0, time.UTC)
	days := eventReportDays(reportEvent{Start: &start, End: &end, Timezone: "Africa/Nairobi"})
	if len(days) != 3 || days[0].Date != "2026-06-17" || days[2].Date != "2026-06-19" {
		t.Fatalf("wrong local dates: %+v", days)
	}
}
func reportTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	connection := os.Getenv("REPORT_TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	db, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}
func TestReportDraftPersistencePostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	app := &config.AppContext{DB: db}
	_, err := db.Exec(ctx, `CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		t.Fatal(err)
	}
	if err = saveEventReportDraft(ctx, app, "event-v2/nairobi", 0, map[string]string{"note": "Nairobi letter"}); err != nil {
		t.Fatal(err)
	}
	if err = saveEventReportDraft(ctx, app, "event-v2/nairobi", 0, map[string]string{"note": "stale"}); !errors.Is(err, errReportConflict) {
		t.Fatal("stale edit not rejected", err)
	}
	if err = saveEventReportDraft(ctx, app, "event-v2/nairobi", 1, map[string]string{"note": "Revised letter"}); err != nil {
		t.Fatal(err)
	}
	var text string
	var revision int
	if err = db.QueryRow(ctx, `SELECT content->>'note',revision FROM sponsor_report_drafts WHERE scope='event-v2/nairobi'`).Scan(&text, &revision); err != nil || text != "Revised letter" || revision != 2 {
		t.Fatal("save failed", text, revision, err)
	}
}
func TestReportMetricsPostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, reportFixtureSQL)
	if err != nil {
		t.Fatal(err)
	}
	page := sponsorReportPage{Event: reportEvent{ID: "event", Tag: "nairobi", Timezone: "Africa/Nairobi"}}
	if err = loadEventReport(ctx, &config.AppContext{DB: db}, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.TeamSizes) != 6 || page.TeamSizes[1].Value != 1 || page.TeamSizes[2].Value != 1 || page.TeamSizes[0].Value != 0 {
		t.Fatal("team-size distribution lost project membership or included drafts")
	}
	if page.Attendees != 2 || page.Registrations != 4 || page.Teams != 2 || page.Participants != 2 || page.Challenges != 1 || page.Sats != 300 || page.Volunteers != 1 || page.Proposals != 2 {
		t.Fatalf("bad metrics: %+v", page)
	}
	if _, err = db.Exec(ctx, `UPDATE registrations SET revoked_before_account_deletion=revoked,revoked=true`); err != nil {
		t.Fatal(err)
	}
	anonymized := sponsorReportPage{Event: page.Event}
	if err = loadEventReport(ctx, &config.AppContext{DB: db}, &anonymized); err != nil {
		t.Fatal(err)
	}
	if anonymized.Attendees != page.Attendees || anonymized.Registrations != page.Registrations {
		t.Fatal("deletion changed historical attendance totals")
	}
	if len(page.Sessions) != 1 || len(page.Speakers) != 1 || len(page.Judges) != 1 || len(page.Winners) != 1 {
		t.Fatalf("bad content counts: sessions %d speakers %d judges %d winners %d", len(page.Sessions), len(page.Speakers), len(page.Judges), len(page.Winners))
	}
	if page.Winners[0].Sats != 300 {
		t.Fatal("duplicate awards multiplied payouts")
	}
	if len(page.Days) != 1 || page.Days[0].Date != "2026-06-17" {
		t.Fatal("session date not used")
	}
	if _, err = db.Exec(ctx, `UPDATE competitions SET results_finalized_at=NULL`); err != nil {
		t.Fatal(err)
	}
	page = sponsorReportPage{Event: reportEvent{ID: "event", Tag: "nairobi", Timezone: "Africa/Nairobi"}}
	if err = loadEventReport(ctx, &config.AppContext{DB: db}, &page); err != nil || len(page.Winners) != 0 {
		t.Fatal("provisional winners published", err)
	}
}

func TestReportFormCSRFPostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, reportFixtureSQL+`
 CREATE TEMP TABLE conferences(id text,tag text,description text,location text,timezone text,date_desc text,start_date timestamptz,end_date timestamptz,publication_status text);
 INSERT INTO conferences VALUES('event','nairobi','Open Source Edition','Nairobi, Kenya','Africa/Nairobi','17–19 June 2026','2026-06-17','2026-06-19','published');
 CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now());`); err != nil {
		t.Fatal(err)
	}
	session := scs.New()
	requestCtx, err := session.Load(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	session.Put(requestCtx, authMethodsCSRFKey, "report-csrf")
	app := &config.AppContext{DB: db, Session: session}
	form := url.Values{"csrf": {"wrong"}, "revision": {"0"}, "note": {"Nairobi letter"}}
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/admin/reports?event=nairobi", strings.NewReader(form.Encode())).WithContext(requestCtx)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		serveSponsorReport(w, r, app)
		return w
	}
	if w := post(); w.Code != 403 {
		t.Fatalf("invalid CSRF response %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM sponsor_report_drafts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("CSRF rejection mutated drafts", err)
	}
	form.Set("csrf", "report-csrf")
	if w := post(); w.Code != 303 {
		t.Fatalf("valid save %d: %s", w.Code, w.Body.String())
	}
	if w := post(); w.Code != 409 {
		t.Fatalf("stale save %d", w.Code)
	}
}

const reportFixtureSQL = `
CREATE TEMP TABLE registrations(conference_id text,revoked boolean,checked_in_at timestamptz,person_id text,email text,revoked_before_account_deletion boolean);
CREATE TEMP TABLE person_emails(person_id text,email text UNIQUE);
CREATE TEMP TABLE competitions(id text,conference_id text,results_finalized_at timestamptz);
CREATE TEMP TABLE projects(id text PRIMARY KEY,competition_id text,status text,title text,short_description text,image_url text,github_url text,demo_url text);
CREATE TEMP TABLE project_members(project_id text,person_id text);
CREATE TEMP TABLE awards(id text,competition_id text,award_type text,sponsored_by_org_id text,status text,archived_at timestamptz,title text);
CREATE TEMP TABLE project_awards(project_id text,award_id text);
CREATE TEMP TABLE award_distributions(competition_id text,project_id text,amount_sats bigint,distribution_type text,status text);
CREATE TEMP TABLE people(id text PRIMARY KEY,name text,norm_photo_path text,company text);
CREATE TEMP TABLE proposals(id text,conference_id text,title text,talk_type text,status text);
CREATE TEMP TABLE conf_talks(id text,conference_id text,proposal_id text,scheduled_start timestamptz,scheduled_end timestamptz,archived_at timestamptz);
CREATE TEMP TABLE recordings(conf_talk_id text,youtube_url text,x_url text);
CREATE TEMP TABLE proposals_speaker_confs(proposal_id text,speaker_conf_id text);
CREATE TEMP TABLE speaker_confs(id text,speaker_id text);
CREATE TEMP TABLE competition_judges(competition_id text,person_id text,public_label_override text,judge_type text);
CREATE TEMP TABLE award_judges(award_id text,person_id text);
CREATE TEMP TABLE volunteers(id text,person_id text);
CREATE TEMP TABLE work_shifts(id text,conference_id text);
CREATE TEMP TABLE work_shifts_volunteers(shift_id text,volunteer_id text);
INSERT INTO registrations(conference_id,revoked,checked_in_at,person_id,email) VALUES ('event',false,now(),'person1','one@example.test'),('event',false,now(),NULL,'alias@example.test'),('event',false,now(),NULL,'other@example.test'),('event',true,now(),NULL,'revoked@example.test'),('event',false,NULL,NULL,'unattended@example.test'),('elsewhere',false,now(),'foreign','foreign@example.test');
INSERT INTO person_emails VALUES ('person1','alias@example.test');
INSERT INTO competitions VALUES ('competition','event',now());
INSERT INTO projects VALUES ('project1','competition','submitted','Winner','Useful build','','https://example.test/repo',''),('project2','competition','advanced','Other','','','',''),('draft','competition','created','Draft','','','','');
INSERT INTO project_members VALUES ('project1','person1'),('project1','person2'),('project2','person1'),('draft','person3');
INSERT INTO awards VALUES ('award1','competition','normal','org','awarded',NULL,'First place'),('award2','competition','challenge','org','awarded',NULL,'Sponsor prize'),('draftaward','competition','challenge','org','draft',NULL,'Draft');
INSERT INTO project_awards VALUES ('project1','award1'),('project1','award2');
INSERT INTO award_distributions VALUES ('competition','project1',100,'sats','sent'),('competition','project1',200,'sats','claimed'),('competition','project1',500,'sats','pending'),('competition','project1',500,'sats','cancelled'),('competition','project1',900,'tickets','sent');
INSERT INTO people VALUES ('person1','Speaker Judge','','Org'),('person2','Teammate','','');
INSERT INTO proposals VALUES ('proposal1','event','Talk','talk','Scheduled'),('proposal2','event','Declined','workshop','Declined'),('proposal3','event','Hacking','hackathon','Scheduled');
INSERT INTO conf_talks VALUES ('session1','event','proposal1','2026-06-17T10:00:00+03','2026-06-17T11:00:00+03',NULL),('archived','event','proposal1',now(),now(),now()),('declined','event','proposal2',now(),now(),NULL);
INSERT INTO proposals_speaker_confs VALUES ('proposal1','speakerconf');
INSERT INTO speaker_confs VALUES ('speakerconf','person1');
INSERT INTO competition_judges VALUES ('competition','person1','','expo'),('competition','person1','','finals');
INSERT INTO award_judges VALUES ('award2','person1');
INSERT INTO volunteers VALUES ('volunteer1','person1'),('volunteer2','person1');
INSERT INTO work_shifts VALUES ('shift1','event'),('shift2','event');
INSERT INTO work_shifts_volunteers VALUES ('shift1','volunteer1'),('shift2','volunteer2');
`
