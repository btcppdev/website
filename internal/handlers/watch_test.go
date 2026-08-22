package handlers

import (
	"strings"
	"testing"
	"time"

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

func TestRecordingBufferPostUsesCanonicalWatchURL(t *testing.T) {
	talk := &RecordingSpeakerCampaignTalk{
		TalkTitle:  "A useful talk",
		YouTubeURL: "https://youtube.com/watch?v=abcdefghijk",
		TalkURL:    "https://btcpp.dev/watch/recording-id",
	}
	got := recordingBufferXText(talk)
	if !strings.Contains(got, talk.TalkURL) {
		t.Fatalf("post does not contain watch URL: %q", got)
	}
	if strings.Contains(got, talk.YouTubeURL) {
		t.Fatalf("post leaked raw YouTube URL: %q", got)
	}
}
