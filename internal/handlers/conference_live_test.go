package handlers

import (
	"bytes"
	"html/template"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConferenceLiveNavigation(t *testing.T) {
	t.Chdir("../..")
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	stale := now.Add(-3 * time.Minute)
	original := &types.Conf{Tag: "berlin26", Desc: "Berlin"}
	conf := hackathonNavConference(original, []*types.Talk{{Status: StatusScheduled, Sched: &types.Times{Start: now}}}, true)
	for _, tc := range []struct {
		name      string
		broadcast *types.ConferenceBroadcast
		live      bool
	}{
		{"missing", nil, false},
		{"live", &types.ConferenceBroadcast{RecordingBroadcast: types.RecordingBroadcast{State: "live", HLSURL: "https://stream.example/live.m3u8", HeartbeatAt: &now}}, true},
		{"stale", &types.ConferenceBroadcast{RecordingBroadcast: types.RecordingBroadcast{State: "live", HLSURL: "https://stream.example/live.m3u8", HeartbeatAt: &stale}}, false},
		{"ended", &types.ConferenceBroadcast{RecordingBroadcast: types.RecordingBroadcast{State: "ended", HLSURL: "https://stream.example/live.m3u8", HeartbeatAt: &now}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			templates, err := ctx.TemplateCache.Clone()
			if err != nil {
				t.Fatal(err)
			}
			templates.Funcs(template.FuncMap{"conferenceIsLive": func(c *types.Conf) bool {
				return c.Tag == conf.Tag && tc.broadcast != nil && recordingBroadcastIsLive(&tc.broadcast.RecordingBroadcast, now)
			}})
			var nav, watch bytes.Buffer
			if err := templates.ExecuteTemplate(&nav, "conference_nav_links", conf); err != nil {
				t.Fatal(err)
			}
			if err := templates.ExecuteTemplate(&watch, "watch.tmpl", conferenceLivePage(conf, tc.broadcast, now)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(watch.String(), nav.String()) {
				t.Fatal("live page navigation differs from conference navigation")
			}
			if strings.Contains(nav.String(), `href="/berlin26/live"`) != tc.live {
				t.Fatalf("unexpected live tab: %s", nav.String())
			}
			for _, tab := range []string{"about", "dates", "hackathon", "speakers", "agenda", "venue", "satellites", "sponsors"} {
				if !strings.Contains(nav.String(), `href="/berlin26#`+tab+`"`) {
					t.Errorf("missing %s navigation tab", tab)
				}
			}
		})
	}
	if original.ShowHackathon || original.HasAgenda {
		t.Fatal("navigation mutated the shared conference record")
	}
}

func TestConferenceLivePageExpiresHeartbeat(t *testing.T) {
	now := time.Now()
	conf := &types.Conf{Tag: "toronto", Desc: "bitcoin++ Toronto"}
	broadcast := &types.ConferenceBroadcast{Title: "Toronto Day 3", RecordingBroadcast: types.RecordingBroadcast{State: "live", HLSURL: "https://stream.example/live.m3u8", HeartbeatAt: &now}}
	page := conferenceLivePage(conf, broadcast, now)
	if page.State != "live" || page.Path != "/toronto/live" || page.Title != "Toronto Day 3" || !page.ConferenceWide {
		t.Fatalf("page=%+v", page)
	}
	page = conferenceLivePage(conf, broadcast, now.Add(3*time.Minute))
	if page.State != "offline" || page.HLSURL != "" {
		t.Fatalf("stale broadcast remains live: %+v", page)
	}
	broadcast.State = "ended"
	if conferenceLivePage(conf, broadcast, now).State != "offline" {
		t.Fatal("ended broadcast remains live")
	}
	if conferenceLivePage(conf, nil, now).State != "offline" {
		t.Fatal("missing broadcast remains live")
	}
}

func TestLegacyConferenceLiveRedirect(t *testing.T) {
	for _, suffix := range []string{"/live", "/live/status"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/conf/toronto"+suffix+"?preview=1", nil)
		redirectStripConfPrefix(w, r)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/toronto"+suffix+"?preview=1" {
			t.Fatalf("redirect status=%d location=%s", w.Code, w.Header().Get("Location"))
		}
	}
}
