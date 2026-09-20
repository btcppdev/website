package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// Optional output makes the real templates available for local browser review.
func TestLivePagePreviewTemplates(t *testing.T) {
	t.Chdir("../..")
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	conf := &types.Conf{Tag: "toronto", Desc: "bitcoin++ consensus edition", DateDesc: "July 22–24, 2026", Location: "Toronto", PublicationStatus: "published"}
	broadcast := &types.ConferenceBroadcast{Title: "Toronto — Day 3", RecordingBroadcast: types.RecordingBroadcast{State: "live", HLSURL: "http://127.0.0.1:18082/demo/index.m3u8", HeartbeatAt: &now, StartedAt: &now}}
	for _, tc := range []struct {
		name, template string
		data           any
		want           string
	}{
		{"live.html", "watch.tmpl", conferenceLivePage(conf, broadcast, now), `id="watch-live-video"`},
		{"offline.html", "watch.tmpl", conferenceLivePage(conf, nil, now), "Currently offline"},
		{"index.html", "embeds/index.tmpl", &HomePageData{Confs: []*types.Conf{conf}, Past: []*types.Conf{conf}, Year: 2026}, "/static/js/live-inset.js"},
	} {
		var out bytes.Buffer
		if err := ctx.TemplateCache.ExecuteTemplate(&out, tc.template, tc.data); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Fatalf("%s missing %s", tc.name, tc.want)
		}
		if dir := os.Getenv("BTCPP_LIVE_PREVIEW_DIR"); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tc.name), out.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
