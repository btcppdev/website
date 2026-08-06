package buffer

import (
	"strings"
	"testing"
	"time"
)

func TestBuildAssetsBlockUsesAssetInputList(t *testing.T) {
	got := buildAssetsBlock([]string{
		"https://cdn.example.com/card.png",
		"https://cdn.example.com/speaker.png",
	})

	if strings.Contains(got, "images") {
		t.Fatalf("assets block used removed Buffer images field: %s", got)
	}
	want := `, assets: [{ image: { url: "https://cdn.example.com/card.png" } }, { image: { url: "https://cdn.example.com/speaker.png" } }]`
	if got != want {
		t.Fatalf("assets block mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestBuildCreatePostMutationUsesExactScheduledTime(t *testing.T) {
	dueAt := time.Date(2026, time.August, 12, 15, 5, 0, 0, time.FixedZone("CDT", -5*60*60))
	got := buildCreatePostMutation("channel-1", "Watch now", []string{"https://cdn.example.com/card.png"}, "twitter", &dueAt)

	for _, want := range []string{
		"mode: customScheduled",
		`dueAt: "2026-08-12T20:05:00Z"`,
		`channelId: "channel-1"`,
		`{ image: { url: "https://cdn.example.com/card.png" } }`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mutation does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "mode: addToQueue") {
		t.Fatalf("scheduled mutation used queue mode:\n%s", got)
	}
}

func TestBuildEditPostMutationPreservesExactScheduleAndAssets(t *testing.T) {
	dueAt := time.Date(2026, time.August, 12, 20, 5, 0, 0, time.UTC)
	got := buildEditPostMutation("post-1", "Updated", []string{"https://cdn.example.com/card.png"}, "twitter", dueAt)
	for _, want := range []string{
		"editPost(input:",
		`id: "post-1"`,
		"mode: customScheduled",
		`dueAt: "2026-08-12T20:05:00Z"`,
		`{ image: { url: "https://cdn.example.com/card.png" } }`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mutation does not contain %q:\n%s", want, got)
		}
	}
}

func TestBuildAssetsBlockEmpty(t *testing.T) {
	if got := buildAssetsBlock(nil); got != "" {
		t.Fatalf("empty assets block = %q, want empty", got)
	}
}
