package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"

	"github.com/alexedwards/scs/v2"
)

func TestAnnualReportSharedTextSelection(t *testing.T) {
	valid := url.Values{"action": {"share_text"}, "share_field": {"community", "community", "lessons"}, "community": {"Shared prose"}, "lessons": {""}}
	selected, err := annualSharedTextSelection(valid)
	if err != nil || !reflect.DeepEqual(selected, []string{"community", "lessons"}) {
		t.Fatal(selected, err)
	}
	for _, form := range []url.Values{
		{"action": {"share_text"}},
		{"action": {"share_text"}, "share_field": {"community"}},
		{"action": {"share_text"}, "share_field": {"community"}, "community": {"one", "two"}},
		{"action": {"share_text"}, "share_field": {"cutoff"}, "cutoff": {"2026-01-01"}},
		{"action": {"share_text"}, "share_field": {"exclude_post_123"}, "exclude_post_123": {"yes"}},
		{"action": {"share_text"}, "share_field": {"event_highlight_../../nydig"}, "event_highlight_../../nydig": {"bad"}},
		{"action": {"share_text"}, "share_field": {"project_built_00000000-0000-0000-0000-000000000000"}, "project_built_00000000-0000-0000-0000-000000000000": {"bad"}},
	} {
		if _, err := annualSharedTextSelection(form); err == nil {
			t.Fatal("invalid sharing selection accepted", form)
		}
	}
	valid.Set("action", "save")
	if selected, err := annualSharedTextSelection(valid); err != nil || len(selected) != 0 {
		t.Fatal("ordinary save shared checked fields", selected, err)
	}
	p := annualFixture()
	p.loadNarrativeSections()
	p.ShareTextFields = []string{"event_highlight_da000000-0000-4000-8000-000000000099"}
	p.Saved[p.ShareTextFields[0]] = "Unrelated event"
	if _, err := p.sharedTextPatch(); err == nil {
		t.Fatal("sharing accepted a field outside this report")
	}
}

func TestAnnualReportSharedTextPostgres(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	app := &config.AppContext{DB: db}
	_, err := db.Exec(ctx, `CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	org := "annual-v1/2026/org/da000000-0000-4000-8000-000000000003"
	frozen := "annual-v1/2026/hrf/frozen/da000000-0000-4000-8000-000000000004"
	untouched := []string{frozen, "annual-v1/2025/hrf", "event-v2/nairobi"}
	for _, scope := range append([]string{"annual-v1/2026/hrf", org}, untouched...) {
		_, err := db.Exec(ctx, `INSERT INTO sponsor_report_drafts(scope,content,updated_at) VALUES($1,$2::jsonb,$3)`, scope,
			`{"community":"Existing sponsor writing","note":"Private letter","cutoff":"2026-01-01","exclude_event_example":"yes","exclude_post_123":"yes"}`, old)
		if err != nil {
			t.Fatal(err)
		}
	}
	read := func(scope string) (map[string]string, int64, time.Time) {
		t.Helper()
		var raw []byte
		var revision int64
		var updated time.Time
		if err := db.QueryRow(ctx, `SELECT content,revision,updated_at FROM sponsor_report_drafts WHERE scope=$1`, scope).Scan(&raw, &revision, &updated); err != nil {
			t.Fatal(err)
		}
		var saved map[string]string
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		return saved, revision, updated
	}
	p := annualFixture()
	p.Saved = map[string]string{"community": "Our shared **year in review**.", "lessons": "", "note": "NYDIG-only letter", "cutoff": "2026-10-19"}
	eventKey := "event_highlight_" + p.Events[0].Event.ID
	projectID := "da000000-0000-4000-8000-000000000005"
	projectKey := "project_built_" + projectID
	p.Events[3].WinningProjects = []reportWinner{{ID: projectID, Rank: 1, Title: "Shared build"}}
	p.Saved[eventKey], p.Saved[projectKey] = "Shared event recap", "Shared project story"
	p.ShareTextFields = []string{"community", "lessons", eventKey, projectKey}
	p.loadNarrativeSections()
	if _, err := saveAnnualReport(ctx, app, p, false); err != nil {
		t.Fatal(err)
	}
	shared, revision, _ := read(annualSharedTextScope(2026))
	if revision != 1 || len(shared) != 4 || shared["community"] != p.Saved["community"] || shared[eventKey] != p.Saved[eventKey] || shared[projectKey] != p.Saved[projectKey] {
		t.Fatal("shared record did not retain exactly the selected fields", shared, revision)
	}
	for _, scope := range []string{"annual-v1/2026/hrf", org} {
		saved, revision, updated := read(scope)
		if saved["community"] != p.Saved["community"] || saved[eventKey] != p.Saved[eventKey] || saved[projectKey] != p.Saved[projectKey] || revision != 2 {
			t.Fatal("selected text did not replace sponsor copy", scope, saved, revision)
		}
		if text, ok := saved["lessons"]; !ok || text != "" {
			t.Fatal("intentional blank lost")
		}
		if saved["note"] != "Private letter" || saved["cutoff"] != "2026-01-01" || saved["exclude_event_example"] != "yes" || saved["exclude_post_123"] != "yes" || !updated.Equal(old) {
			t.Fatal("sharing changed recipient-only text or reporting scope", saved, updated)
		}
	}
	for _, scope := range untouched {
		saved, revision, updated := read(scope)
		if saved["community"] != "Existing sponsor writing" || revision != 1 || !updated.Equal(old) {
			t.Fatal("sharing crossed its year/live-draft boundary", scope)
		}
	}
	future := annualReportPage{Year: 2026, Recipient: "Future sponsor", Saved: map[string]string{"note": "Personal greeting"}}
	if err := loadAnnualSharedText(ctx, app, &future); err != nil {
		t.Fatal(err)
	}
	if future.SharedTextRevision != 1 || future.Saved["community"] != p.Saved["community"] || future.Saved["note"] != "Personal greeting" {
		t.Fatal("new sponsor did not inherit shared text", future)
	}
	future.Saved["community"] = ""
	if err := loadAnnualSharedText(ctx, app, &future); err != nil || future.Saved["community"] != "" {
		t.Fatal("recipient-specific blank was overwritten", err)
	}
	stale := annualFixture()
	stale.Recipient = "HRF"
	stale.Revision = 2 // Even a matching draft revision cannot bypass a stale shared revision.
	stale.Saved["community"] = "Old tab"
	if _, err := saveAnnualReport(ctx, app, stale, false); !errors.Is(err, errReportConflict) {
		t.Fatal("old tab overwrote shared writing", err)
	}
	p.Revision, p.SharedTextRevision, p.ShareTextFields = 1, 1, nil
	p.Saved["community"] = "NYDIG-specific follow-up"
	if _, err := saveAnnualReport(ctx, app, p, false); err != nil {
		t.Fatal(err)
	}
	saved, _, _ := read("annual-v1/2026/hrf")
	if saved["community"] != shared["community"] {
		t.Fatal("ordinary save leaked to another sponsor")
	}
	// A failed destination update must roll back the source save and shared row.
	_, err = db.Exec(ctx, `CREATE FUNCTION pg_temp.reject_report_share() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated destination failure'; END $$;
 CREATE TRIGGER reject_share BEFORE UPDATE ON sponsor_report_drafts FOR EACH ROW WHEN (OLD.scope='annual-v1/2026/hrf') EXECUTE FUNCTION pg_temp.reject_report_share()`)
	if err != nil {
		t.Fatal(err)
	}
	p.Revision = 2
	p.ShareTextFields = []string{"community"}
	p.Saved["community"] = "Must roll back everywhere"
	if _, err := saveAnnualReport(ctx, app, p, false); err == nil {
		t.Fatal("destination failure ignored")
	}
	original, rev, _ := read(p.Scope())
	stillShared, sharedRev, _ := read(annualSharedTextScope(2026))
	if original["community"] != "NYDIG-specific follow-up" || rev != 2 || stillShared["community"] != shared["community"] || sharedRev != 1 {
		t.Fatal("sharing partially committed")
	}
}

func TestAnnualReportSharedTextHTTP(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, reportFixtureSQL+`CREATE TEMP TABLE conferences(id text,tag text,description text,location text,timezone text,date_desc text,start_date timestamptz,end_date timestamptz,publication_status text);
 CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now());
 INSERT INTO organizations VALUES('da000000-0000-4000-8000-000000000009','New recipient','','')`)
	if err != nil {
		t.Fatal(err)
	}
	session := scs.New()
	requestCtx, err := session.Load(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	session.Put(requestCtx, authMethodsCSRFKey, "shared-csrf")
	source, err := os.ReadFile("../../templates/admin/annual_report.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("admin/annual_report.tmpl").Parse(string(source))
	if err == nil {
		_, err = tmpl.ParseFiles("../../templates/admin/reports.tmpl")
	}
	if err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{DB: db, Session: session, TemplateCache: tmpl, Err: log.New(io.Discard, "", 0)}
	request := func(method, target string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(form.Encode())).WithContext(requestCtx)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		serveAnnualSponsorReport(w, r, app)
		return w
	}
	form := url.Values{"csrf": {"shared-csrf"}, "revision": {"0"}, "shared_text_revision": {"0"}, "action": {"freeze"}, "cutoff": {"2026-01-01"}, "community": {"Frozen original"}}
	w := request("POST", "/admin/reports/annual?year=2026&recipient=HRF", form)
	if w.Code != 303 {
		t.Fatalf("freeze: %d %s", w.Code, w.Body.String())
	}
	frozenURL := w.Header().Get("Location")
	form.Set("action", "share_text")
	form.Set("community", "Writing shared from NYDIG")
	form.Set("note", "NYDIG private letter")
	form.Set("share_field", "community")
	form.Set("csrf", "wrong")
	target := "/admin/reports/annual?year=2026&recipient=NYDIG"
	if w := request("POST", target, form); w.Code != 403 {
		t.Fatal("share bypassed CSRF", w.Code)
	}
	form.Set("csrf", "shared-csrf")
	w = request("POST", target, form)
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "shared=1") {
		t.Fatalf("share: %d %s", w.Code, w.Body.String())
	}
	w = request("GET", "/admin/reports/annual?year=2026&recipient=HRF", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Writing shared from NYDIG") || strings.Contains(w.Body.String(), "NYDIG private letter") {
		t.Fatal("shared HTTP draft wrong", w.Code)
	}
	w = request("GET", frozenURL, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Frozen original") || strings.Contains(w.Body.String(), "Writing shared from NYDIG") || strings.Contains(w.Body.String(), "annual-share-writing") {
		t.Fatal("sharing changed frozen edition", w.Code)
	}
	newURL := "/admin/reports/annual?year=2026&recipient=da000000-0000-4000-8000-000000000009"
	if w := request("POST", newURL, form); w.Code != 409 {
		t.Fatal("stale new-recipient form overwrote shared text", w.Code)
	}
	w = request("GET", newURL, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Writing shared from NYDIG") || !strings.Contains(w.Body.String(), `name="shared_text_revision" value="1"`) {
		t.Fatal("new report did not inherit shared copy", w.Code)
	}
	form.Set("action", "save")
	form.Set("shared_text_revision", "1")
	form.Set("community", "Custom new-recipient text")
	if w := request("POST", newURL, form); w.Code != 303 {
		t.Fatal("recipient customization failed", w.Code, w.Body.String())
	}
	w = request("GET", target, nil)
	if !strings.Contains(w.Body.String(), "Writing shared from NYDIG") || strings.Contains(w.Body.String(), "Custom new-recipient text") {
		t.Fatal("ordinary save propagated checked fields")
	}
}
