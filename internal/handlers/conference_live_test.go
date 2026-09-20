package handlers

import (
	"btcpp-web/internal/types"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
