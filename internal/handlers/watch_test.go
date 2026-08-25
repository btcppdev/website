package handlers

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestRecordingWatchPath(t *testing.T) {
	if got, want := recordingWatchPath("recording id"), "/watch/recording%20id"; got != want {
		t.Fatalf("recordingWatchPath() = %q, want %q", got, want)
	}
	if got := recordingWatchPath("  "); got != "" {
		t.Fatalf("recordingWatchPath(empty) = %q", got)
	}
}

func TestRecordingWatchState(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	tests := []struct {
		name string
		rec  *types.Recording
		want string
	}{
		{name: "scheduled", rec: &types.Recording{YTLink: "https://youtu.be/abcdefghijk", PublishAt: &future}, want: "scheduled"},
		{name: "available", rec: &types.Recording{YTLink: "https://youtu.be/abcdefghijk"}, want: "available"},
		{name: "processing", rec: &types.Recording{}, want: "processing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recordingWatchState(tt.rec, now); got != tt.want {
				t.Fatalf("recordingWatchState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRecordingBroadcastLiveRequiresFreshHeartbeat(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Minute)
	stale := now.Add(-3 * time.Minute)
	broadcast := &types.RecordingBroadcast{State: "live", HLSURL: "https://stream.btcpp.dev/live/test.m3u8", HeartbeatAt: &fresh}
	if !recordingBroadcastIsLive(broadcast, now) {
		t.Fatal("fresh broadcast heartbeat was not live")
	}
	broadcast.HeartbeatAt = &stale
	if recordingBroadcastIsLive(broadcast, now) {
		t.Fatal("stale broadcast heartbeat remained live")
	}
	broadcast.HeartbeatAt = &fresh
	broadcast.HLSURL = "javascript:alert(1)"
	if recordingBroadcastIsLive(broadcast, now) {
		t.Fatal("unsafe HLS URL was accepted")
	}
}

func TestRecordingWatchDescriptionTruncatesMetadata(t *testing.T) {
	description := strings.Repeat("word ", 80)
	got := recordingWatchDescription(&types.ConfTalk{Proposal: &types.Proposal{Description: description}}, nil)
	if len([]rune(got)) > 180 || !strings.HasSuffix(got, "…") {
		t.Fatalf("description was not safely truncated: %q", got)
	}
}

func TestLiveTickerTitleTruncatesToFiftyCharacters(t *testing.T) {
	got := liveTickerTitle(strings.Repeat("a", 60))
	if len([]rune(got)) != 50 || !strings.HasSuffix(got, "...") {
		t.Fatalf("liveTickerTitle() = %q (%d runes)", got, len([]rune(got)))
	}
}

func TestLiveTickerSpeakerLinks(t *testing.T) {
	got := liveTickerSpeakerLinks([]*types.Speaker{
		{Name: "Both", Github: "https://github.com/example", Twitter: types.ParseTwitter("@example_x")},
		{Name: "GitHub Only", Github: "github-only"},
		{Name: "Name Only"},
	})
	if len(got) != 3 {
		t.Fatalf("liveTickerSpeakerLinks() = %+v", got)
	}
	if got[0].Provider != "X" || got[0].Handle != "example_x" || got[0].URL != "https://x.com/example_x" {
		t.Fatalf("preferred X link = %+v", got[0])
	}
	if got[1].Provider != "GitHub" || got[1].Handle != "github-only" || got[1].URL != "https://github.com/github-only" {
		t.Fatalf("GitHub fallback = %+v", got[1])
	}
	if got[2].Provider != "Name" || got[2].Name != "Name Only" || got[2].URL != "" {
		t.Fatalf("name fallback = %+v", got[2])
	}
}

func TestLiveStatusCacheCollapsesLookupsAndExpires(t *testing.T) {
	var cache liveStatusCache
	now := time.Date(2026, 8, 25, 18, 0, 0, 0, time.UTC)
	loads := 0
	load := func() (liveStatusResponse, error) {
		loads++
		return liveStatusResponse{Live: true, Title: "Cached talk"}, nil
	}
	for i := 0; i < 3; i++ {
		response, err := cache.get(now.Add(time.Duration(i)*time.Second), load)
		if err != nil || !response.Live {
			t.Fatalf("cache response = %+v, err = %v", response, err)
		}
	}
	if loads != 1 {
		t.Fatalf("loader called %d times inside TTL, want 1", loads)
	}
	if _, err := cache.get(now.Add(liveStatusCacheTTL), load); err != nil {
		t.Fatal(err)
	}
	if loads != 2 {
		t.Fatalf("loader called %d times after expiry, want 2", loads)
	}
}

func TestLiveStatusCacheDoesNotCacheErrors(t *testing.T) {
	var cache liveStatusCache
	now := time.Date(2026, 8, 25, 18, 0, 0, 0, time.UTC)
	loads := 0
	load := func() (liveStatusResponse, error) {
		loads++
		if loads == 1 {
			return liveStatusResponse{}, errors.New("temporary failure")
		}
		return liveStatusResponse{}, nil
	}
	if _, err := cache.get(now, load); err == nil {
		t.Fatal("expected loader error")
	}
	if _, err := cache.get(now, load); err != nil {
		t.Fatalf("retry after loader error: %v", err)
	}
	if loads != 2 {
		t.Fatalf("loader called %d times, want error to remain uncached", loads)
	}
}

func TestLiveStatusIsPubliclyCacheable(t *testing.T) {
	siteLiveStatusCache.mu.Lock()
	previousResponse := siteLiveStatusCache.response
	previousExpiry := siteLiveStatusCache.expiresAt
	siteLiveStatusCache.response = liveStatusResponse{}
	siteLiveStatusCache.expiresAt = time.Now().Add(time.Minute)
	siteLiveStatusCache.mu.Unlock()
	t.Cleanup(func() {
		siteLiveStatusCache.mu.Lock()
		siteLiveStatusCache.response = previousResponse
		siteLiveStatusCache.expiresAt = previousExpiry
		siteLiveStatusCache.mu.Unlock()
	})

	response := httptest.NewRecorder()
	LiveStatus(response, httptest.NewRequest("GET", "/live/status", nil), &config.AppContext{})
	if got, want := response.Header().Get("Cache-Control"), "public, max-age=5, s-maxage=15, stale-while-revalidate=30"; got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
	if got := strings.TrimSpace(response.Body.String()); got != `{"live":false}` {
		t.Fatalf("body = %q", got)
	}
}
