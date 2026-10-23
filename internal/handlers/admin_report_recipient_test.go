package handlers

import (
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"html/template"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestReportRecipientAndEditableIntroductions(t *testing.T) {
	p := reportFixturePage(t)
	form := url.Values{"prepared_for": {"  HRF  "}, "projects_intro": {"**A new project introduction.**"}}
	saved, err := eventReportForm(form, &p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p.Saved = saved
	hydrateEventReport(&p)
	if p.PreparedFor() != "HRF" || p.Section("projects_intro").Text != "**A new project introduction.**" {
		t.Fatal("recipient or narrative not retained")
	}
	form.Set("prepared_for", strings.Repeat("x", 201))
	if _, err = eventReportForm(form, &p, time.Now()); err == nil {
		t.Fatal("oversized recipient accepted")
	}
	p.Saved["prepared_for"] = "<script>alert(1)</script>"
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = tmpl.ExecuteTemplate(&out, "report_paper", p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>alert(1)</script>") || !strings.Contains(out.String(), "&lt;script&gt;") {
		t.Fatal("recipient not escaped")
	}
	p.Saved["prepared_for"] = ""
	if p.PreparedFor() != "Our partners" {
		t.Fatal("missing generic fallback")
	}
}

func TestReportSponsorSelection(t *testing.T) {
	p := reportFixturePage(t)
	p.Sponsors = []reportSponsor{{ID: "sponsor-a", Name: "Sponsor A", Logo: "https://example.test/logo.svg"}}
	form := url.Values{"prepared_for_org_id": {"sponsor-a"}, "prepared_for": {"Tampered name"}}
	saved, err := eventReportForm(form, &p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p.Saved = saved
	if p.PreparedFor() != "Sponsor A" || p.RecipientLogo() != "https://example.test/logo.svg" {
		t.Fatal("sponsor identity or logo lost")
	}
	form.Set("prepared_for_org_id", "another-event-sponsor")
	if _, err = eventReportForm(form, &p, time.Now()); err == nil {
		t.Fatal("unrelated sponsor accepted")
	}
	form.Set("prepared_for_org_id", "")
	saved, err = eventReportForm(form, &p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p.Saved = saved
	if p.PreparedFor() != "Our partners" || p.RecipientLogo() != "" {
		t.Fatal("clearing recipient kept logo")
	}
	p.Saved = map[string]string{"prepared_for": "sponsor a"}
	if p.SelectedSponsor().ID != "sponsor-a" {
		t.Fatal("legacy name not matched")
	}
	p.Saved = map[string]string{"prepared_for": "Legacy sponsor"}
	form.Set("prepared_for_org_id", "legacy")
	saved, err = eventReportForm(form, &p, time.Now())
	if err != nil || saved["prepared_for"] != "Legacy sponsor" {
		t.Fatal("legacy recipient lost", err)
	}
	for _, raw := range []string{"javascript:alert(1)", "//evil.test/logo", "data:text/html,test"} {
		if reportSponsorLogo(raw) != "" {
			t.Fatal("unsafe logo accepted")
		}
	}
}

func TestReportSponsorEventScope(t *testing.T) {
	db := reportTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, reportFixtureSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO organizations VALUES('one','One','https://example.test/one.svg',''),('two','Two','','https://example.test/two.svg');
 INSERT INTO sponsorships VALUES('s1','one',NULL),('s2','one',NULL),('s3','two',NULL),('archived','two',now());
 INSERT INTO sponsorships_conferences VALUES('s1','event'),('s2','event'),('s3','other-event'),('archived','event');`); err != nil {
		t.Fatal(err)
	}
	p := sponsorReportPage{Event: reportEvent{ID: "event"}}
	if err := loadReportSponsors(ctx, &config.AppContext{DB: db}, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Sponsors) != 1 || p.Sponsors[0].ID != "one" {
		t.Fatalf("wrong event sponsors: %+v", p.Sponsors)
	}
}

func TestReportBtrustDarkLogo(t *testing.T) {
	s := reportSponsor{ID: "btrust", Name: "Btrust", Logo: "/static/img/sponsors/btrust.svg"}
	if reportRecipientLogo(s) != "/static/img/sponsors/btrust-dark.svg" {
		t.Fatal("Btrust report mark must be dark on light paper")
	}
	p := sponsorReportPage{Sponsors: []reportSponsor{s}, Saved: map[string]string{"prepared_for_org_id": "btrust"}}
	if p.RecipientLogo() != reportRecipientLogo(s) {
		t.Fatal("selected recipient used wrong logo")
	}
}
