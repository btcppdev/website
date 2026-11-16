package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"html"
	"html/template"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
)

func TestAnnualReportRecipientKeys(t *testing.T) {
	for raw, want := range map[string]string{"": "NYDIG", " nydig ": "NYDIG", "hrf": "HRF", "DA000000-0000-4000-8000-000000000001": "da000000-0000-4000-8000-000000000001"} {
		key, err := annualRecipientKey(raw)
		if err != nil || key != want {
			t.Fatalf("key(%q) = %q, %v", raw, key, err)
		}
	}
	for _, raw := range []string{"unknown", "../nydig", "NYDIG/frozen/123", "00000000-0000-0000-0000-000000000000"} {
		if _, err := annualRecipientKey(raw); err == nil {
			t.Fatalf("invalid recipient accepted: %s", raw)
		}
	}
	legacy := annualReportPage{Year: 2026, Recipient: "NYDIG"}
	if legacy.Scope() != "annual-v1/2026/nydig" || legacy.URL() != "/admin/reports/annual?year=2026&recipient=NYDIG" {
		t.Fatal("legacy links changed")
	}
	p := annualReportPage{Year: 2026, Recipient: "Research / Community", RecipientKey: "da000000-0000-4000-8000-000000000001"}
	want := "annual-v1/2026/org/" + p.RecipientKey
	if p.Scope() != want || !strings.HasSuffix(p.URL(), "recipient="+p.RecipientKey) {
		t.Fatal("organization name became the storage key")
	}
	p.Recipient = "Renamed organization"
	if p.Scope() != want {
		t.Fatal("rename changed draft identity")
	}
	if strings.Contains(p.ClosingDeck(), "backing") || strings.Contains(annualSections(2026, p.Recipient)[0].Text, "sponsor") {
		t.Fatal("recipient selection invented sponsorship")
	}
}

func TestAnnualReportAnyOrganizationHTTP(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, reportFixtureSQL+`
CREATE TEMP TABLE conferences(id text,tag text,description text,location text,timezone text,date_desc text,start_date timestamptz,end_date timestamptz,publication_status text);
CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now());
INSERT INTO organizations VALUES
('da000000-0000-4000-8000-000000000001','Research & Community','/static/img/example.svg',''),
('da000000-0000-4000-8000-000000000002','Research & Community','',''),
('da000000-0000-4000-8000-000000000003','Btrust','',''),
('da000000-0000-4000-8000-000000000004','NYDIG','',''),
('da000000-0000-4000-8000-000000000005','Unsafe logo','javascript:alert(1)','');`)
	if err != nil {
		t.Fatal(err)
	}
	session := scs.New()
	requestCtx, err := session.Load(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	session.Put(requestCtx, authMethodsCSRFKey, "annual-csrf")
	source, err := annualRecipientTestTemplate()
	if err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{DB: db, Session: session, TemplateCache: source, Err: log.New(io.Discard, "", 0)}
	request := func(method, target string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, target, strings.NewReader(form.Encode())).WithContext(requestCtx)
		if method == "POST" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		w := httptest.NewRecorder()
		serveAnnualSponsorReport(w, r, app)
		return w
	}
	const first = "da000000-0000-4000-8000-000000000001"
	const second = "da000000-0000-4000-8000-000000000002"
	target := "/admin/reports/annual?year=2026&recipient=" + first
	w := request("GET", target, nil)
	if w.Code != 200 {
		t.Fatalf("non-sponsor report failed: %d %s", w.Code, w.Body.String())
	}
	for _, text := range []string{`value="` + first + `" selected`, `value="` + second + `"`, `value="da000000-0000-4000-8000-000000000003"`, `value="NYDIG"`, `value="HRF"`, "Research &amp; Community", "/static/img/example.svg"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatalf("missing recipient choice/branding %q", text)
		}
	}
	main := strings.Split(w.Body.String(), "<main>")[1]
	if strings.Contains(main, "NYDIG") || strings.Contains(main, "HRF") || strings.Contains(main, "headline sponsorship") || strings.Contains(main, "For backing") {
		t.Fatal("another sponsor's branding or funding claim leaked")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM sponsor_report_drafts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("opening a report wrote a draft", err)
	}

	form := url.Values{"csrf": {"annual-csrf"}, "revision": {"0"}, "action": {"freeze"}, "cutoff": {"2026-01-01"}, "note": {"Letter for the first organization."}}
	form.Set("csrf", "invalid")
	if w := request("POST", target, form); w.Code != 403 {
		t.Fatal("new recipient bypassed CSRF", w.Code)
	}
	form.Set("csrf", "annual-csrf")
	w = request("POST", target, form)
	if w.Code != 303 {
		t.Fatalf("new recipient freeze: %d %s", w.Code, w.Body.String())
	}
	frozenURL := w.Header().Get("Location")
	if !strings.Contains(frozenURL, "recipient="+first) || !strings.Contains(frozenURL, "edition=") {
		t.Fatal("frozen URL lost organization identity", frozenURL)
	}
	if w := request("POST", target, form); w.Code != 409 {
		t.Fatal("stale organization draft accepted", w.Code)
	}
	form.Set("action", "save")
	form.Set("note", "Independent letter for the second organization.")
	if w := request("POST", strings.Replace(target, first, second, 1), form); w.Code != 303 {
		t.Fatal("same-name organizations shared a draft", w.Code)
	}
	var raw []byte
	edition, _ := url.Parse(frozenURL)
	if err := db.QueryRow(ctx, `SELECT content FROM sponsor_report_drafts WHERE scope=$1`, "annual-v1/2026/org/"+first+"/frozen/"+edition.Query().Get("edition")).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var frozen annualReportPage
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.RecipientOrganizationID != first || frozen.RecipientLogo() != "/static/img/example.svg" || frozen.CSRF != "" || len(frozen.Recipients) != 0 || strings.Contains(string(raw), second) {
		t.Fatal("snapshot lost recipient identity or stored the directory")
	}
	if _, err := db.Exec(ctx, `UPDATE organizations SET name='Renamed Research',logo_light_url='/static/img/changed.svg' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	w = request("GET", target, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Renamed Research") || !strings.Contains(w.Body.String(), "Letter for the first organization.") {
		t.Fatal("rename lost the saved report", w.Code)
	}
	for _, deleted := range []bool{false, true} {
		if deleted {
			if _, err := db.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, first); err != nil {
				t.Fatal(err)
			}
		}
		w = request("GET", frozenURL, nil)
		if w.Code != 200 {
			t.Fatal("frozen report stopped working", w.Code)
		}
		body := strings.Split(w.Body.String(), "<main>")[1]
		if !strings.Contains(body, html.EscapeString("Research & Community")) || !strings.Contains(body, "/static/img/example.svg") || strings.Contains(body, "Renamed Research") || strings.Contains(body, "changed.svg") {
			t.Fatal("organization change rewrote frozen branding")
		}
	}
	if w := request("GET", strings.Replace(frozenURL, first, second, 1), nil); w.Code != 404 {
		t.Fatal("frozen report crossed organization scope", w.Code)
	}
	for _, key := range []string{first, "da000000-0000-4000-8000-000000000099", "../nydig"} {
		if w := request("GET", "/admin/reports/annual?year=2026&recipient="+url.QueryEscape(key), nil); w.Code != 400 {
			t.Fatal("invalid/deleted organization accepted", key, w.Code)
		}
	}
	for key, logo := range map[string]string{"da000000-0000-4000-8000-000000000003": "/static/img/sponsors/btrust-dark.svg", "da000000-0000-4000-8000-000000000005": ""} {
		choices, err := loadAnnualReportRecipients(ctx, app)
		if err != nil {
			t.Fatal(err)
		}
		p := annualReportPage{Recipients: choices}
		if !p.selectRecipient(key) || p.RecipientLogo() != logo {
			t.Fatal("unsafe or missing logo handling", key)
		}
	}
}

func annualRecipientTestTemplate() (*template.Template, error) {
	// ParseFiles names the root after the basename; the production handler uses
	// the cached admin/... name instead.
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl", "../../templates/admin/reports.tmpl")
	if err != nil {
		return nil, err
	}
	_, err = tmpl.New("admin/annual_report.tmpl").Parse(`{{template "annual_report.tmpl" .}}`)
	return tmpl, err
}

func TestAnnualReportOrganizationAttribution(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, reportFixtureSQL+`
CREATE TEMP TABLE conferences(id text,tag text,description text,location text,timezone text,date_desc text,start_date timestamptz,end_date timestamptz,publication_status text);
CREATE TEMP TABLE sponsor_report_drafts(scope text PRIMARY KEY,content jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1,updated_at timestamptz NOT NULL DEFAULT now());
INSERT INTO conferences VALUES ('event','nairobi','Open Source','Nairobi','UTC','','2026-06-17','2026-06-19','published');
INSERT INTO organizations VALUES ('first','Same name','',''),('second','Same name','',''),('unfunded','Community organization','','');
INSERT INTO sponsorships VALUES ('first-paid','first',NULL,'Paid','Community'),('second-paid','second',NULL,'Paid','Headline');
INSERT INTO sponsorships_conferences VALUES ('first-paid','event'),('second-paid','event');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second", "unfunded"} {
		p := annualReportPage{Year: 2026, Recipient: "Renamed organization", RecipientOrganizationID: id, Cutoff: "2026-10-19", Saved: map[string]string{"hack_event": "records"}}
		if err := loadAnnualReport(ctx, &config.AppContext{DB: db}, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Included()) != 1 || p.ParticipationTotal() == 0 {
			t.Fatal("non-headline or unfunded organization lost the year's event data")
		}
		if id == "unfunded" {
			if len(p.PartnerEvents) != 0 || p.PartnerSummary() != "" {
				t.Fatal("recipient selection created a sponsorship claim")
			}
		} else if len(p.PartnerEvents) != 1 || p.PartnerEvents[0].Headline != (id == "second") {
			t.Fatal("attribution mixed same-name organizations or depended on a mutable name", id, p.PartnerEvents)
		}
	}
}
