package handlers

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestAgendaTalkPermalinks(t *testing.T) {
	t.Chdir("../..")
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	var days []*AgendaDay
	for i := 1; i <= 3; i++ {
		start := time.Date(2028, time.October, i, 14, 0, 0, 0, time.UTC)
		end := start.Add(45 * time.Minute)
		session := &types.Session{Name: fmt.Sprintf("Talk %d: Building a Bitcoin POS With Merchants, Not For Them", i), Type: "talk", Speakers: []*types.Speaker{{Name: "Example Speaker", Twitter: types.Twitter{Handle: "example"}, Github: "example", Website: "https://example.org", Nostr: "npub1example", LinkedIn: "https://www.linkedin.com/in/example", Instagram: "example", LeetCode: "example"}, {Name: "Walter Maffione"}, {Name: "Steven Roose"}}, AnchorTag: fmt.Sprintf("talk-%d", i), ConfTag: "berlin26", Venue: "one", Sched: &types.Times{Start: start.Add(30 * time.Minute), End: &end}, Description: "A shareable talk."}
		days = append(days, &AgendaDay{Idx: i, Date: start, Active: i == 1, Info: &types.ConfInfo{Venues: []string{"one", "two"}}, All: []*types.Session{session, {Name: "Scaling Bitcoin with Swap Service Providers", AnchorTag: fmt.Sprintf("parallel-%d", i), ConfTag: "berlin26", Venue: "two", Sched: &types.Times{Start: start, End: &end}, Type: "panel", Speakers: session.Speakers}}})
	}
	for _, tc := range []struct{ name, dayTemplate string }{
		{"berlin26", "generic_conf_agenda_day_rows"},
		{"berlin26/agenda", "public_conf_agenda_grid_day"},
	} {
		templates, err := ctx.TemplateCache.Clone()
		if err != nil {
			t.Fatal(err)
		}
		_, err = templates.New("agenda_link_preview").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/static/css/btcpp-redesign.css"><script defer src="/static/js/btcpp-redesign.js"></script></head><body class="btcpp-rebrand-page"><section id="agenda"><div class="tabs"><div role="tablist">{{range .}}<a href="#agenda-day-{{.Idx}}" data-agenda-tab="{{.Idx}}">Day {{.Idx}}</a>{{end}}</div>{{range .}}{{template "` + tc.dayTemplate + `" .}}{{end}}</div></section></body></html>`)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if tc.name == "berlin26/agenda" {
			page := &ConfPage{Conf: &types.Conf{Tag: "berlin26", Desc: "bitcoin++ Berlin, payments edition"}, AgendaDays: days}
			if err := templates.ExecuteTemplate(&out, "conf/agenda.tmpl", page); err != nil {
				t.Fatal(err)
			}
		} else if err := templates.ExecuteTemplate(&out, "agenda_link_preview", days); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 3; i++ {
			for _, want := range []string{fmt.Sprintf(`href="/berlin26#talk-%d"`, i), fmt.Sprintf(`id="agenda-dialog-talk-%d"`, i), "data-agenda-share"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("%s missing %s", tc.name, want)
				}
			}
		}
		for i := 1; i <= 3; i++ {
			if strings.Count(out.String(), fmt.Sprintf(`id="agenda-dialog-talk-%d"`, i)) != 1 {
				t.Errorf("%s must render each talk dialog exactly once", tc.name)
			}
		}
		if tc.name == "berlin26/agenda" && !strings.Contains(out.String(), `--agenda-venue-count: 2`) {
			t.Error("agenda omitted the simultaneous room columns")
		}
		if strings.Contains(out.String(), "rebrand-agenda-strip") {
			t.Errorf("%s still includes agenda strip", tc.name)
		}
		if dir := os.Getenv("BTCPP_AGENDA_PREVIEW_DIR"); dir != "" {
			target := filepath.Join(dir, tc.name, "index.html")
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, out.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
