package handlers

import (
	"bytes"
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
