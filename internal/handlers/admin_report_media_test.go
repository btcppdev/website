package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestReportMediaIdentity(t *testing.T) {
	for _, raw := range []string{"https://youtube.com/watch?v=abcdefghijk", "https://youtu.be/abcdefghijk", "https://www.youtube.com/live/abcdefghijk"} {
		p, id := reportMediaIdentity(raw)
		if p != "YouTube" || id != "abcdefghijk" {
			t.Fatalf("%s: %s %s", raw, p, id)
		}
	}
	for _, raw := range []string{"http://youtube.com/watch?v=abcdefghijk", "https://youtube.com.evil.test/watch?v=abcdefghijk", "https://x.com/i/broadcasts/abc/extra", "https://user@youtube.com/watch?v=abcdefghijk", "https://youtu.be/invalid"} {
		p, _ := reportMediaIdentity(raw)
		if p != "" {
			t.Fatalf("accepted %s", raw)
		}
	}
	p, id := reportMediaIdentity("https://x.com/i/broadcasts/1OxwbbMVymvJB")
	if p != "X" || id != "1OxwbbMVymvJB" {
		t.Fatal(p, id)
	}
}

func TestReportMediaPreservedAndRendered(t *testing.T) {
	media := []reportMedia{{Platform: "YouTube", ID: "abcdefghijk", Event: "Nairobi", Title: "Example recording", URL: "https://youtube.com/watch?v=abcdefghijk", MomentURL: "https://youtube.com/watch?v=abcdefghijk&t=135s", Views: 42, Collected: "2026-10-22", Basis: "Lifetime views"}}
	raw, _ := json.Marshal(media)
	p := sponsorReportPage{Saved: map[string]string{"media_snapshot": string(raw)}}
	saved, err := eventReportForm(url.Values{}, &p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if saved["media_snapshot"] != string(raw) {
		t.Fatal("normal save lost snapshot")
	}
	form := url.Values{"media_links": {"https://youtu.be/abcdefghijk 135"}}
	if err := saveReportMediaFields(form, saved, p.Saved); err != nil {
		t.Fatal(err)
	}
	form.Set("media_links", "https://x.com/i/broadcasts/123 135")
	if saveReportMediaFields(form, saved, p.Saved) == nil {
		t.Fatal("unsupported X timestamp accepted")
	}
	tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "report_media_page", p.MediaPages()[0]); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Nairobi", "Watch selected moment", "135s"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(out.String(), "42") {
		t.Fatal("individual counter leaked into highlight card")
	}
	annual := annualFixture()
	annual.Events[0].Media = media
	tmpl, err = template.ParseFiles("../../templates/admin/annual_report.tmpl", "../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := tmpl.Execute(&out, annual); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Example recording") {
		t.Fatal("annual media missing")
	}
}

func TestReportMediaFailurePreservesSnapshot(t *testing.T) {
	previous := []reportMedia{{Platform: "X", ID: "abc", Views: 123, Collected: "2026-10-22"}}
	raw, _ := json.Marshal(previous)
	saved := map[string]string{"media_snapshot": string(raw), "media_links": "https://x.com/i/broadcasts/abc"}
	refreshReportMedia(context.Background(), &config.AppContext{Env: &types.EnvConfig{}}, &sponsorReportPage{}, saved)
	var got []reportMedia
	if err := json.Unmarshal([]byte(saved["media_snapshot"]), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Views != 123 || got[0].Collected != "2026-10-22" {
		t.Fatalf("failed refresh changed prior data: %+v", got)
	}
	if !strings.Contains(saved["media_status"], "previous snapshot retained") {
		t.Fatal("missing refresh warning")
	}
}
