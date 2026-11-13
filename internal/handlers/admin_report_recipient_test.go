package handlers

import (
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"fmt"
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
	if _, err := db.Exec(ctx, `INSERT INTO organizations VALUES
 ('one','One','https://example.test/one.svg',''),('two','Two','','https://example.test/two.svg'),
 ('pending','Pending partner','',''),('committed','Committed partner','','https://example.test/committed.svg');
 INSERT INTO sponsorships(id,organization_id,archived_at,status,level) VALUES
 ('s1','one',NULL,' Paid ','silver'),('s2','one',NULL,'Pending','Headline'),('s3','two',NULL,'Paid','Headline'),('archived','two',now(),'Paid','Headline'),
 ('s4','one',NULL,'Committed',' Title Sponsors '),('s5','one',NULL,'Paid','Headline'),('s6','one',now(),'Paid','Diamond'),
 ('pending','pending',NULL,'Pending','Headline'),('committed','committed',NULL,'committed','Bronze');
 INSERT INTO sponsorships_conferences VALUES('s1','event'),('s2','event'),('s3','other-event'),('archived','event'),('pending','event'),('committed','event'),('s4','event'),('s5','other-event'),('s6','event');`); err != nil {
		t.Fatal(err)
	}
	p := sponsorReportPage{Event: reportEvent{ID: "event"}}
	if err := loadReportSponsors(ctx, &config.AppContext{DB: db}, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Sponsors) != 3 {
		t.Fatalf("wrong event sponsors: %+v", p.Sponsors)
	}
	if got := p.EventSponsors(); len(got) != 2 || got[0].ID != "one" || got[0].Level != "Title" || got[1].ID != "committed" || got[1].Level != "Bronze" || got[1].Logo != "https://example.test/committed.svg" {
		t.Fatalf("acknowledgments must use each sponsor's highest confirmed tier for this event: %+v", got)
	}
	p.Saved = map[string]string{"prepared_for_org_id": "pending"}
	if p.SelectedSponsor().ID != "pending" {
		t.Fatal("recipient choices should retain all attached organizations")
	}
	if err := loadReportSponsors(ctx, &config.AppContext{DB: db}, &p); err != nil || len(p.Sponsors) != 3 {
		t.Fatal("reloading duplicated sponsors", err)
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

func TestReportSponsorAcknowledgments(t *testing.T) {
	p := reportFixturePage(t)
	p.Sponsors = []reportSponsor{
		{ID: "selected", Name: "Selected partner", Logo: "/static/selected.svg", Level: "Headline", Confirmed: true},
		{ID: "other", Name: "Community <partner>", Level: "Community", Confirmed: true},
		{ID: "untiered", Name: "Untiered partner", Confirmed: true},
		{ID: "pending", Name: "Unconfirmed prospect"},
	}
	p.Saved["prepared_for_org_id"] = "selected"
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	rosters := strings.Split(out.String(), `<div class="report-sponsor-grid">`)
	if len(rosters) != 3 {
		t.Fatal("both paper and slide editions must acknowledge event sponsors")
	}
	for _, part := range rosters[1:] {
		roster := strings.SplitN(part, "</section>", 2)[0]
		if !strings.Contains(roster, "Selected partner") || !strings.Contains(roster, "Community &lt;partner&gt;") || strings.Contains(roster, "Unconfirmed prospect") {
			t.Fatalf("incorrect sponsor acknowledgment: %s", roster)
		}
		if !strings.Contains(roster, `report-sponsor--rank-1"`) || !strings.Contains(roster, `report-sponsor--rank-5"`) || strings.Index(roster, "Selected partner") > strings.Index(roster, "Community &lt;partner&gt;") {
			t.Fatalf("sponsor marks must be ordered and sized by tier: %s", roster)
		}
		if strings.Index(roster, "Untiered partner") > strings.Index(roster, "Community &lt;partner&gt;") {
			t.Fatal("untiered sponsors should use the standard size before community partners")
		}
	}
	if strings.Contains(out.String(), "NYDIG · HRF · Btrust") {
		t.Fatal("unrelated fixed sponsor names remain in event footers")
	}
	p.Sponsors = nil
	if len(p.SponsorPages()) != 0 {
		t.Fatal("no sponsors must not create an empty report page")
	}
}

func TestReportSponsorPagination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		levels []string
		counts []int
	}{
		{"standard", make([]string, 25), []int{12, 12, 1}},
		{"headline", strings.Fields(strings.Repeat("Headline ", 25)), []int{6, 6, 6, 6, 1}},
		{"all tiers", []string{"Headline", "Headline", "Title", "Workshop", "Hackathon", "Bronze", "Bronze", "Media", "Community"}, []int{9}},
		{"community", strings.Fields(strings.Repeat("Community ", 25)), []int{10, 10, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p sponsorReportPage
			for i, level := range tc.levels {
				p.Sponsors = append(p.Sponsors, reportSponsor{ID: fmt.Sprint(i), Name: fmt.Sprintf("Partner %02d", i), Level: level, Confirmed: true})
			}
			pages := p.SponsorPages()
			if len(pages) != len(tc.counts) {
				t.Fatalf("pages=%d, want %d", len(pages), len(tc.counts))
			}
			seen := map[string]bool{}
			for i, page := range pages {
				count := 0
				for _, row := range page.Rows {
					for _, sponsor := range row.Sponsors {
						if seen[sponsor.ID] || sponsor.DisplayRank() != row.Rank {
							t.Fatal("sponsor repeated or placed in the wrong tier")
						}
						seen[sponsor.ID] = true
						count++
					}
				}
				if count != tc.counts[i] {
					t.Fatalf("page %d has %d sponsors, want %d", i, count, tc.counts[i])
				}
			}
			if len(seen) != len(tc.levels) {
				t.Fatal("pagination dropped sponsors")
			}
		})
	}
}
