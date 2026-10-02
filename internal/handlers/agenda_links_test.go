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
	for i := 1; i <= 2; i++ {
		start := time.Date(2028, time.October, i, 12, 0, 0, 0, time.UTC)
		end := start.Add(time.Hour)
		session := &types.Session{Name: fmt.Sprintf("Talk %d", i), AnchorTag: fmt.Sprintf("talk-%d", i), ConfTag: "berlin26", Venue: "Main Stage", Sched: &types.Times{Start: start, End: &end}, Description: "A shareable talk."}
		days = append(days, &AgendaDay{Idx: i, Date: start, Active: i == 1, Info: &types.ConfInfo{Venues: []string{"Main Stage"}}, All: []*types.Session{session}})
	}
	for _, tc := range []struct{ name, dayTemplate string }{
		{"berlin26", "generic_conf_agenda_day_rows"},
		{"berlin26/agenda", "public_conf_agenda_grid_day"},
	} {
		templates, err := ctx.TemplateCache.Clone()
		if err != nil {
			t.Fatal(err)
		}
		_, err = templates.New("agenda_link_preview").Parse(`<!doctype html><html><head><link rel="stylesheet" href="/static/css/btcpp-redesign.css"><script defer src="/static/js/btcpp-redesign.js"></script></head><body class="btcpp-rebrand-page"><section id="agenda"><div class="tabs"><div role="tablist">{{range .}}<a href="#agenda-day-{{.Idx}}" data-agenda-tab="{{.Idx}}">Day {{.Idx}}</a>{{end}}</div>{{range .}}{{template "` + tc.dayTemplate + `" .}}{{end}}</div></section></body></html>`)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := templates.ExecuteTemplate(&out, "agenda_link_preview", days); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 2; i++ {
			for _, want := range []string{fmt.Sprintf(`href="/berlin26#talk-%d"`, i), fmt.Sprintf(`id="agenda-dialog-talk-%d"`, i), "data-agenda-share"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("%s missing %s", tc.name, want)
				}
			}
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
