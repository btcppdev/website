package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
)

func annualFixture() annualReportPage {
	p := annualReportPage{Year: 2026, Recipient: "NYDIG", Cutoff: "2026-10-19", Generated: "2026-10-19T20:00:00Z", Saved: map[string]string{}, RegistrationBased: true, UniqueRegistrants: 1090, RepeatRegistrants: 115, UniqueSpeakers: 178, UniqueParticipants: 150, Preview: true}
	p.Sections = annualSections(p.Year, p.Recipient)
	for i, city := range []string{"Floripa", "Vienna", "Las Vegas", "Nairobi", "Toronto", "Berlin"} {
		start := time.Date(2026, time.Month(i+4), 12, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 0, 2)
		e := annualEvent{Event: reportEvent{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), Tag: strings.ToLower(strings.ReplaceAll(city, " ", "")), Location: city, Title: "Illustrative event", Start: &start, End: &end, Timezone: "UTC"}, Included: true, Eligible: true, Attendees: int64(100 + i*20), Registrations: int64(150 + i*25), Talks: int64(20 + i*3), Workshops: int64(4 + i), Panels: 3, HackCoverage: "records", Projects: int64(10 + i*4), Participants: int64(25 + i*8), Winners: 3, Challenges: 4, Sats: 5000000}
		e.Event.Period = reportEventPeriod(e.Event)
		if i == 1 || i == 2 {
			e.HackCoverage = "none"
			e.Projects = 0
			e.Participants = 0
			e.Winners = 0
			e.Sats = 0
		}
		if i == 0 {
			e.HackCoverage = "unknown"
		}
		if i == 3 {
			e.WinningProjects = []reportWinner{{ID: "demo", Title: "Illustrative winning project", Awards: "First place", Description: "A synthetic project card for testing the report composition.", GitHub: "https://example.test/project"}}
		}
		p.Events = append(p.Events, e)
	}
	return p
}

func TestAnnualReportRender(t *testing.T) {
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	p := annualFixture()
	p.Sections[2].Text = "**Technical conversations** <script>alert(1)</script>"
	for _, frozen := range []bool{false, true} {
		p.Frozen = ""
		if frozen {
			p.Frozen = p.Generated
		}
		var out bytes.Buffer
		if err = tmpl.Execute(&out, p); err != nil {
			t.Fatal(err)
		}
		body := out.String()
		for _, want := range []string{"Registrations by event", "No hackathon", "Data not yet reconciled", "Annual export not supplied", "Illustrative winning project", "<strong>Technical conversations</strong>", "/nairobi/hackathon/projects/demo"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %q", want)
			}
		}
		if strings.Contains(body, "<script>alert(1)</script>") || strings.Contains(body, "ZgotmplZ") {
			t.Fatal("unsafe or invalid template output")
		}
		if strings.Contains(body, `id="report-edit-form"`) == frozen {
			t.Fatal("frozen edition exposes editor")
		}
	}
}

func TestAnnualReportBoundaries(t *testing.T) {
	start := time.Date(2026, 12, 31, 9, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	e := reportEvent{Start: &start, End: &end, Timezone: "Pacific/Auckland"}
	if annualEligible(e, 2026, "2026-12-31") {
		t.Fatal("event ends next local year")
	}
	end = start.Add(time.Hour)
	if !annualEligible(e, 2026, "2026-12-31") {
		t.Fatal("same local day should be included")
	}
	e.End = nil
	if annualEligible(e, 2026, "2026-12-31") {
		t.Fatal("unknown end must not count as completed")
	}
	p := annualFixture()
	if len(p.ProjectChart().Rows) != 3 || len(p.ParticipationChart().Rows) != 6 {
		t.Fatal("unreviewed/no-hackathon events included in hackathon bars")
	}
	if p.ReturningChart().DonutTotal() != p.UniqueRegistrants {
		t.Fatal("registration donut double counts")
	}
	form := url.Values{"action": {"save"}, "cutoff": {"2026-10-19"}, "note": {"Different sponsor letter"}}
	if err := annualReportForm(&p, form, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	form.Set("reach_youtube_views", "500")
	if annualReportForm(&p, form, time.Now()) == nil {
		t.Fatal("unattributed annual analytics accepted")
	}
	form.Del("reach_youtube_views")
	form.Set("cutoff", "2027-01-01")
	if annualReportForm(&p, form, time.Now()) == nil {
		t.Fatal("wrong year accepted")
	}
}

func TestAnnualReportAuthentication(t *testing.T) {
	session := scs.New()
	ctx, err := session.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		AdminAnnualSponsorReport(w, httptest.NewRequest(method, "/admin/reports/annual", nil).WithContext(ctx), &config.AppContext{Session: session})
		if w.Code != 303 {
			t.Fatalf("anonymous access: %d", w.Code)
		}
	}
}

func TestAnnualReportTemplateCache(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{Env: &types.EnvConfig{}, Err: log.New(os.Stderr, "", 0)}
	if err = loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	renderAnnualReport(w, app, annualFixture())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "A year of") {
		t.Fatalf("annual template missing from production cache: %d %s", w.Code, w.Body.String())
	}
}

func TestAnnualReportCSRFAndFrozenHTTP(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TEMP TABLE conferences(id text,tag text,description text,location text,timezone text,date_desc text,start_date timestamptz,end_date timestamptz,publication_status text);
CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now());`); err != nil {
		t.Fatal(err)
	}
	session := scs.New()
	requestCtx, err := session.Load(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	session.Put(requestCtx, authMethodsCSRFKey, "annual-csrf")
	source, err := os.ReadFile("../../templates/admin/annual_report.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("admin/annual_report.tmpl").Parse(string(source))
	if err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{DB: db, Session: session, TemplateCache: tmpl, Err: log.New(os.Stderr, "", 0)}
	form := url.Values{"csrf": {"wrong"}, "revision": {"0"}, "action": {"freeze"}, "cutoff": {"2026-01-01"}, "note": {"Frozen annual letter"}}
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/admin/reports/annual?year=2026&recipient=HRF", strings.NewReader(form.Encode())).WithContext(requestCtx)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		serveAnnualSponsorReport(w, r, app)
		return w
	}
	if w := post(); w.Code != 403 {
		t.Fatalf("CSRF accepted: %d", w.Code)
	}
	form.Set("csrf", "annual-csrf")
	w := post()
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "edition=") {
		t.Fatalf("freeze failed: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", w.Header().Get("Location"), nil).WithContext(requestCtx)
	frozen := httptest.NewRecorder()
	serveAnnualSponsorReport(frozen, r, app)
	if frozen.Code != 200 || !strings.Contains(frozen.Body.String(), "Frozen annual letter") || strings.Contains(frozen.Body.String(), `id="report-edit-form"`) {
		t.Fatalf("frozen route failed: %d %s", frozen.Code, frozen.Body.String())
	}
	r.URL.RawQuery = strings.Replace(r.URL.RawQuery, "recipient=HRF", "recipient=NYDIG", 1)
	wrong := httptest.NewRecorder()
	serveAnnualSponsorReport(wrong, r, app)
	if wrong.Code != 404 {
		t.Fatal("frozen edition crossed recipient scope", wrong.Code)
	}
	if w := post(); w.Code != 409 {
		t.Fatalf("stale form accepted: %d", w.Code)
	}
}

func TestAnnualReportPostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	app := &config.AppContext{DB: db}
	_, err := db.Exec(ctx, reportFixtureSQL+`
CREATE TEMP TABLE conferences(id text,tag text,description text,location text,timezone text,date_desc text,start_date timestamptz,end_date timestamptz,publication_status text);
CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now());
INSERT INTO conferences VALUES ('event','nairobi','Open Source','Nairobi','Africa/Nairobi','','2026-06-17','2026-06-19','published'),('elsewhere','toronto','Toronto','Toronto','UTC','','2026-08-01','2026-08-03','published'),('future','seoul','Seoul','Seoul','UTC','','2026-11-01','2026-11-03','published');
INSERT INTO registrations VALUES ('elsewhere',false,now(),'person1','one@example.test',NULL),('elsewhere',false,now(),NULL,'alias@example.test',NULL);
INSERT INTO proposals VALUES ('toronto-talk','elsewhere','Another talk','workshop','Accepted');
INSERT INTO conf_talks(id,conference_id,proposal_id) VALUES ('toronto-session','elsewhere','toronto-talk');
INSERT INTO proposals_speaker_confs VALUES ('toronto-talk','speakerconf');
INSERT INTO recordings VALUES ('session1','https://youtube.com/watch?v=one',''),('session1','https://youtube.com/watch?v=two','');`)
	if err != nil {
		t.Fatal(err)
	}
	p := annualReportPage{Year: 2026, Recipient: "NYDIG", Cutoff: "2026-10-19", Generated: "2026-10-19T20:00:00Z", CSRF: "do-not-persist", Saved: map[string]string{"hack_event": "records", "note": "Original letter"}}
	if err = loadAnnualReport(ctx, app, &p); err != nil {
		t.Fatal(err)
	}
	if p.UniqueRegistrants != 4 || p.RepeatRegistrants != 1 || p.ParticipationTotal() != 5 || p.UniqueSpeakers != 1 || p.UniqueParticipants != 2 {
		t.Fatalf("incorrect annual deduplication: %+v", p)
	}
	if len(p.Included()) != 2 || len(p.Upcoming()) != 1 || p.ProgramChart().DonutTotal() != 2 {
		t.Fatal("future event or duplicate recording inflated totals")
	}
	if len(p.Events[0].Reach) != 1 {
		t.Fatal("Nairobi default evidence was not carried through")
	}
	id, err := saveAnnualReport(ctx, app, p, true)
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	if _, err = saveAnnualReport(ctx, app, p, true); !errors.Is(err, errReportConflict) {
		t.Fatal("stale freeze accepted", err)
	}
	p.Revision = 1
	p.Saved["note"] = "Updated live draft"
	p.UniqueRegistrants = 999
	if _, err = saveAnnualReport(ctx, app, p, false); err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err = db.QueryRow(ctx, `SELECT content FROM sponsor_report_drafts WHERE scope=$1`, p.Scope()+"/frozen/"+id).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var frozen annualReportPage
	if err = json.Unmarshal(data, &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.UniqueRegistrants != 4 || frozen.Saved["note"] != "Original letter" || frozen.CSRF != "" {
		t.Fatal("frozen edition changed or stored a CSRF token")
	}
	p.Recipient = "HRF"
	p.Revision = 0
	p.Saved = map[string]string{"note": "HRF letter"}
	if _, err = saveAnnualReport(ctx, app, p, false); err != nil {
		t.Fatal("recipient drafts collide", err)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM sponsor_report_drafts WHERE scope LIKE '%/frozen/%'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed freeze left an orphan edition", count, err)
	}
}

// Explicit opt-in, loopback-only visual preview. No database writes or production bypass.
func TestAnnualReportPreview(t *testing.T) {
	if os.Getenv("ANNUAL_REPORT_PREVIEW") != "1" {
		t.Skip("local visual preview")
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../../static"))))
	mux.HandleFunc("/admin/reports/preview", renderReportMarkdownPreview)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Preview only", 405)
			return
		}
		p := annualFixture()
		if r.URL.Query().Get("recipient") == "HRF" {
			p.Recipient = "HRF"
			p.Sections = annualSections(p.Year, p.Recipient)
		}
		tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err = tmpl.Execute(w, p); err != nil {
			log.Print(err)
		}
	})
	t.Log("Annual preview: http://127.0.0.1:8140/admin/reports/annual")
	t.Fatal(http.ListenAndServe("127.0.0.1:8140", mux))
}

func TestAnnualReportLegacyFrozenParticipation(t *testing.T) {
	p := annualFixture()
	p.RegistrationBased = false
	p.Frozen = "2026-10-19"
	p.UniqueAttendees, p.ReturningAttendees = 790, 95
	if p.ParticipationChart().Rows[0].Value != p.Events[0].Attendees || p.ReturningChart().DonutTotal() != 790 {
		t.Fatal("legacy snapshot metrics were changed")
	}
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Check-ins by event") || strings.Contains(out.String(), "Registrations by event") {
		t.Fatal("legacy snapshot was relabeled")
	}
	p.RegistrationBased = true
	if p.ParticipationChart().Rows[0].Value != p.Events[0].Registrations || p.ReturningChart().DonutTotal() != p.UniqueRegistrants {
		t.Fatal("registration charts used check-ins")
	}
}
