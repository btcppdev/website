package handlers

import (
	"fmt"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestSocialCardTalkTitleUsesPrefixBeforeColon(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "colon",
			title: "Routing Nodes: What We Learned",
			want:  "Routing Nodes",
		},
		{
			name:  "trims prefix",
			title: "  Lightning Liquidity  : Practical Lessons  ",
			want:  "Lightning Liquidity",
		},
		{
			name:  "no colon",
			title: "Mining Policy for Operators",
			want:  "Mining Policy for Operators",
		},
		{
			name:  "empty prefix",
			title: ": A Subtitle Without a Prefix",
			want:  ": A Subtitle Without a Prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := socialCardTalkTitle(tt.title); got != tt.want {
				t.Fatalf("socialCardTalkTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestTalkCardsChangedOnlyWhenSourceHashChanges(t *testing.T) {
	cardHashesMu.Lock()
	oldHashes := cardHashes
	cardHashes = make(map[string]string)
	cardHashesMu.Unlock()
	talkManifestMu.Lock()
	oldManifest, oldFetchedAt := talkManifest, talkManifestFetchedAt
	talkManifest = map[string]string{"clip.png": "clip-v1"}
	talkManifestFetchedAt = time.Now()
	talkManifestMu.Unlock()
	t.Cleanup(func() {
		cardHashesMu.Lock()
		cardHashes = oldHashes
		cardHashesMu.Unlock()
		talkManifestMu.Lock()
		talkManifest, talkManifestFetchedAt = oldManifest, oldFetchedAt
		talkManifestMu.Unlock()
	})

	talk := &types.Talk{ID: "talk-1", Event: "toronto", Name: "Original title", Clipart: "clip.png"}
	key := fmt.Sprintf("%s/talks/%s-1080p.png", talk.Event, talk.ID)
	cardHashesMu.Lock()
	cardHashes[key] = talkCardHash(talk)
	cardHashesMu.Unlock()

	if talkCardsChanged(talk, "1080p") {
		t.Fatal("unchanged talk was marked for regeneration")
	}
	talk.Name = "Updated title"
	if !talkCardsChanged(talk, "1080p") {
		t.Fatal("updated talk title was not marked for regeneration")
	}
}
