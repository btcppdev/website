package buffer

import (
	"encoding/json"
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

func TestBuildCreateMediaPostMutation(t *testing.T) {
	for _, tc := range []struct {
		name           string
		assets         []Asset
		want, postType string
	}{
		{"image first", []Asset{{URL: "https://cdn.example.com/extra.png", Kind: "image"}, {URL: "https://cdn.example.com/card.png", Kind: "image"}}, `assets: [{ image: { url: "https://cdn.example.com/extra.png" } }, { image: { url: "https://cdn.example.com/card.png" } }]`, "carousel"},
		{"video reel", []Asset{{URL: "https://cdn.example.com/clip.mp4", Kind: "video"}}, `assets: [{ video: { url: "https://cdn.example.com/clip.mp4" } }]`, "reel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildCreateMediaPostMutation("channel", "copy", tc.assets, "instagram", nil)
			if !strings.Contains(got, tc.want) || !strings.Contains(got, "type: "+tc.postType) {
				t.Fatalf("unexpected mutation: %s", got)
			}
		})
	}
}

func TestXTicketThreadIncludesOriginalPostAndMedia(t *testing.T) {
	const original = "Meet Ada at bitcoin++"
	const reply = "Tickets are going fast -> https://btcpp.dev/berlin26#tickets"
	replyJSON, _ := json.Marshal(reply)
	for _, kind := range []string{"image", "video"} {
		t.Run(kind, func(t *testing.T) {
			got := buildCreateMediaPostMutation("x-channel", original, []Asset{{URL: "https://cdn.test/first", Kind: kind}}, "twitter", nil, reply)
			want := `twitter: { thread: [{ text: "Meet Ada at bitcoin++", assets: [{ ` + kind + `: { url: "https://cdn.test/first" } }] }, { text: ` + string(replyJSON) + ` }] }`
			if !strings.Contains(got, want) {
				t.Fatalf("thread missing original post, media, or second reply: %s", got)
			}
			if strings.Count(got, `text: "`+original+`"`) != 2 || strings.Count(got, "assets:") != 1 {
				t.Fatalf("original text or media duplicated incorrectly: %s", got)
			}
		})
	}
	for _, service := range []string{"instagram", "linkedin"} {
		got := buildCreateMediaPostMutation("channel", original, nil, service, nil, reply)
		if strings.Contains(got, "thread:") || strings.Contains(got, reply) {
			t.Fatalf("X reply leaked to %s: %s", service, got)
		}
	}
}
