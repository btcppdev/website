package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestPublicEventCountsInclude2022AndExcludeDrafts(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	confs := []*types.Conf{
		{Tag: "atx22", PublicationStatus: "published", StartDate: time.Date(2022, 6, 2, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2022, 6, 5, 0, 0, 0, 0, time.UTC)},
		{Tag: "cdmx22", PublicationStatus: "published", StartDate: time.Date(2022, 12, 10, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2022, 12, 12, 0, 0, 0, 0, time.UTC)},
		{Tag: "draft", PublicationStatus: "draft", StartDate: now.AddDate(0, 0, -10), EndDate: now.AddDate(0, 0, -9)},
		nil,
	}
	for i := 0; i < 19; i++ {
		confs = append(confs, &types.Conf{Tag: fmt.Sprintf("edition-%d", i), PublicationStatus: "published", StartDate: now.AddDate(0, 0, -30-i), EndDate: now.AddDate(0, 0, -29-i)})
	}
	nav := navConfListAt(confs, now)
	if len(nav.Past) != 21 || len(nav.Upcoming) != 0 {
		t.Fatalf("past=%v upcoming=%v", confTags(nav.Past), confTags(nav.Upcoming))
	}
	if nav.Past[19].Tag != "cdmx22" || nav.Past[20].Tag != "atx22" {
		t.Fatalf("oldest editions missing or out of order: %v", confTags(nav.Past))
	}
	ticker := template.New("ticker").Funcs(template.FuncMap{
		"msg":            func(key string) string { return key },
		"insiderVisible": func() bool { return false },
	})
	ticker, err := ticker.ParseFiles("../../templates/section/site_ticker.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := ticker.ExecuteTemplate(&out, "site_ticker_items", nav); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "21 EDITIONS IN THE ARCHIVE") {
		t.Fatalf("ticker count does not match the published archive: %s", out.String())
	}

	page := HomePageData{Confs: confs, Years: homeTimelineYears(confs)}
	archive := template.New("archive").Funcs(template.FuncMap{
		"dict":      func(...any) map[string]any { return nil },
		"confImage": func(string, string) string { return "" },
	})
	archive, err = archive.Parse(`{{define "site_seo"}}{{end}}{{define "mainnav"}}{{end}}{{define "generic_conf_footer"}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	archive, err = archive.ParseFiles("../../templates/embeds/timeline.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := archive.ExecuteTemplate(&out, "timeline.tmpl", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "21 editions since 2022") || strings.Count(out.String(), `<article class="timeline-edition">`) != 21 {
		t.Fatal("archive headline and public cards disagree")
	}

	// New published editions join the archive only after their event grace period.
	confs = append(confs, &types.Conf{Tag: "next", PublicationStatus: "published", StartDate: now.AddDate(0, 1, 0), EndDate: now.AddDate(0, 1, 2)})
	nav = navConfListAt(confs, now)
	if len(nav.Past) != 21 || len(nav.Upcoming) != 1 || nav.Upcoming[0].Tag != "next" {
		t.Fatal("upcoming event incorrectly counted in the archive")
	}
	nav = navConfListAt(confs, now.AddDate(0, 2, 0))
	if len(nav.Past) != 22 || len(nav.Upcoming) != 0 {
		t.Fatal("archive count did not grow after the next event")
	}
}
