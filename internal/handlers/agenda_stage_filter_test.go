package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/chromedp/chromedp"
)

func TestPublicAgendaFilters(t *testing.T) {
	t.Chdir("../..")
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	var days []*AgendaDay
	for i := 1; i <= 2; i++ {
		start := time.Date(2028, time.October, i, 10, 0, 0, 0, time.UTC)
		end := start.Add(30 * time.Minute)
		venues := []string{"one", "two"}
		if i == 2 {
			venues = append(venues, "three")
		}
		day := &AgendaDay{Idx: i, Date: start, Active: i == 1, Info: &types.ConfInfo{Venues: venues}}
		for j, venue := range venues {
			day.All = append(day.All, &types.Session{
				Name:      []string{"Zebra foundations", "Alpha privacy", "Middle workshop"}[j],
				Speakers:  []*types.Speaker{{Name: []string{"Bob", "Zoe", "Alice"}[j]}},
				Type:      []string{"talk", "panel", "workshop"}[j],
				AnchorTag: fmt.Sprintf("talk-%s-%d", venue, i), ConfTag: "example", Venue: venue,
				Sched: &types.Times{Start: start, End: &end},
			})
		}
		days = append(days, day)
	}
	fixtures := make(map[string]string)
	for _, view := range []string{"grid", "list"} {
		var out bytes.Buffer
		name := "conf/agenda.tmpl"
		if view == "list" {
			name = "generic_conf_agenda"
		}
		page := &ConfPage{Conf: &types.Conf{Tag: "example", Desc: "Example conference", HasAgenda: true}, AgendaDays: days}
		if err := app.TemplateCache.ExecuteTemplate(&out, name, page); err != nil {
			t.Fatal(err)
		}
		body := out.String()
		// Render the actual agenda and assets, excluding unrelated page scripts and
		// external fonts. No database, account or external service is needed.
		if view == "grid" {
			_, section, ok := strings.Cut(body, `<section class="rebrand-agenda-browser">`)
			if !ok {
				t.Fatal("agenda section missing")
			}
			section, _, ok = strings.Cut(section, "</section>")
			if !ok {
				t.Fatal("agenda section not closed")
			}
			body = `<section class="rebrand-agenda-browser">` + section + "</section>"
		}
		for _, attr := range []string{"data-agenda-stage-filter", "data-agenda-type-filter", "data-agenda-search", "data-agenda-reset", "data-agenda-session"} {
			if !strings.Contains(body, attr) {
				t.Fatalf("%s agenda missing %s", view, attr)
			}
		}
		if strings.Contains(body, "data-agenda-sort") != (view == "list") {
			t.Fatal("only the list should offer sorting")
		}
		if !strings.Contains(body, "data-agenda-filter-controls hidden") {
			t.Fatal("controls must stay hidden without JavaScript")
		}
		fixtures["/"+view] = `<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/static/css/btcpp-redesign.css"><script defer src="/static/js/btcpp-redesign.js"></script></head><body class="btcpp-rebrand-page">` + body + `</body></html>`
	}
	var browser string
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			browser = path
			break
		}
	}
	if browser == "" {
		t.Skip("interaction checks require Chromium or Chrome; template checks passed")
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, fixtures[r.URL.Path]) })
	server := httptest.NewServer(mux)
	defer server.Close()
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(browser))...)
	defer cancelAllocator()
	for _, view := range []string{"grid", "list"} {
		for _, width := range []int64{390, 1280} {
			t.Run(fmt.Sprintf("%s/%d", view, width), func(t *testing.T) {
				ctx, cancel := chromedp.NewContext(allocator)
				defer cancel()
				ctx, timeout := context.WithTimeout(ctx, 20*time.Second)
				defer timeout()
				check := func(label, expression string) chromedp.Action[chromedp.Void] {
					return chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
						ok, err := chromedp.Evaluate[bool](expression)(ctx, target)
						if err != nil {
							return fmt.Errorf("%s: %w", label, err)
						}
						if !ok {
							return fmt.Errorf("agenda filters: %s", label)
						}
						return nil
					})
				}
				if err := chromedp.Do(ctx,
					chromedp.EmulateViewport(width, 844),
					chromedp.Navigate(server.URL+"/"+view),
					check("controls initialized and labelled", `(() => { window.controls = document.querySelector('[data-agenda-filter-controls]'); window.field = name => controls.querySelector('[data-agenda-' + name + ']'); window.set = (name, value) => { const el = field(name); el.value = value; el.dispatchEvent(new Event('input', {bubbles:true})); }; window.shown = day => Array.from(document.querySelectorAll('#agenda-day-' + day + ' [data-agenda-session]:not([hidden])')); return !controls.hidden && Array.from(controls.querySelectorAll('input, select')).every(el => el.labels.length > 0); })()`),
					check("deduplicated options across all days", `Array.from(field('stage-filter').options, o => o.textContent).join('|') === 'All stages|Main Stage|Talks Stage|Workshops Stage' && field('type-filter').options.length === 4`),
					check("default schedule is intact", `shown(1).length === 2 && field('result-count').textContent === '2'`),
					check("stage filters sessions", `(() => { set('stage-filter', 'two'); return shown(1).length === 1 && shown(1)[0].dataset.agendaVenue === 'two'; })()`),
					check("grid columns collapse", `(() => { const grid = document.querySelector('#agenda-day-1 .rebrand-schedule-grid'); return !grid || (grid.style.getPropertyValue('--agenda-venue-count') === '1' && grid.querySelectorAll('.rebrand-schedule-head[data-agenda-stage]:not([hidden])').length === 1 && grid.getBoundingClientRect().width <= innerWidth); })()`),
					check("filters persist across days", `(() => { document.querySelector('[data-agenda-tab="2"]').click(); return field('stage-filter').value === 'two' && shown(2).length === 1 && field('result-count').textContent === '1'; })()`),
					check("later-day stage works", `(() => { set('stage-filter','three'); return shown(2).length === 1 && shown(2)[0].dataset.agendaVenue === 'three'; })()`),
					check("empty state when stage is absent", `(() => { document.querySelector('[data-agenda-tab="1"]').click(); return shown(1).length === 0 && !document.querySelector('#agenda-day-1 [data-agenda-empty]').hidden && field('result-count').textContent === '0'; })()`),
					check("combined type and multiword speaker/title search", `(() => { set('stage-filter',''); set('type-filter','panel'); set('search','  ALPHA  zoe  '); return shown(1).length === 1 && shown(1)[0].dataset.agendaVenue === 'two'; })()`),
					check("conflicting filters show no results", `(() => { set('type-filter','talk'); return shown(1).length === 0 && !document.querySelector('#agenda-day-1 [data-agenda-empty]').hidden; })()`),
					check("reset restores all filters", `(() => { field('reset').click(); return shown(1).length === 2 && field('search').value === '' && field('type-filter').value === '' && field('stage-filter').value === ''; })()`),
					check("title, speaker and time ordering", `(() => { if (!field('sort')) return true; document.querySelector('[data-agenda-tab="2"]').click(); set('sort','title'); if (shown(2).map(s => s.dataset.agendaVenue).join() !== 'two,three,one') return false; set('sort','speaker'); if (shown(2).map(s => s.dataset.agendaVenue).join() !== 'three,one,two') return false; field('reset').click(); return field('sort').value === 'time' && shown(2).map(s => s.dataset.agendaVenue).join() === 'one,two,three'; })()`),
					chromedp.Evaluate[chromedp.Void](`document.querySelector('[data-agenda-tab="1"]').click(); set('stage-filter','one'); set('type-filter','workshop'); set('search','no match'); location.hash = 'talk-two-2';`),
					chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
						_, err := chromedp.Poll[bool](`document.querySelector('#agenda-dialog-talk-two-2').classList.contains('is-open')`)(ctx, target)
						return err
					}),
					check("permalink reveals hidden talk and its day", `field('stage-filter').value === 'two' && field('search').value === '' && field('type-filter').value === '' && document.querySelector('#agenda-day-2').classList.contains('w--tab-active') && shown(2).length === 1`),
					chromedp.Evaluate[chromedp.Void](`document.querySelector('#agenda-dialog-talk-two-2 .btcpp-agenda-dialog__close').click()`),
					chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
						_, err := chromedp.Poll[bool](`document.querySelector('#agenda-dialog-talk-two-2').getAttribute('aria-hidden') === 'true'`)(ctx, target)
						return err
					}),
				); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestAgendaGridFilterUsesDisplayedVenue(t *testing.T) {
	t.Chdir("../..")
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	day := &AgendaDay{Idx: 1, Info: &types.ConfInfo{Venues: []string{"three"}}, All: []*types.Session{{Name: "Imported talk", AnchorTag: "imported", Venue: ""}}}
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "public_conf_agenda_grid_day", day); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `data-agenda-session data-agenda-venue="three"`) {
		t.Fatal("imported talk must filter under its displayed Workshops Stage column")
	}
	out.Reset()
	if err := app.TemplateCache.ExecuteTemplate(&out, "public_conf_agenda_grid_day", &AgendaDay{Idx: 2}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "data-agenda-empty") {
		t.Fatal("days without configured venues need an empty state for filtering")
	}
}
