package handlers

import (
	"fmt"
	"testing"
	"time"

	"btcpp-web/internal/config"
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

func TestMediaUpdatesEnabledOnlyInProduction(t *testing.T) {
	if mediaUpdatesEnabled(nil) {
		t.Fatal("nil context enabled media updates")
	}
	if mediaUpdatesEnabled(&config.AppContext{}) {
		t.Fatal("non-production context enabled media updates")
	}
	if !mediaUpdatesEnabled(&config.AppContext{InProduction: true}) {
		t.Fatal("production context did not enable media updates")
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

func TestSponsorCardHashUsesSpacesManifestFingerprint(t *testing.T) {
	sponsorManifestMu.Lock()
	oldManifest, oldFetchedAt := sponsorManifest, sponsorManifestFetchedAt
	sponsorManifest = map[string]string{"logo.png": "logo-v1"}
	sponsorManifestFetchedAt = time.Now()
	sponsorManifestMu.Unlock()
	t.Cleanup(func() {
		sponsorManifestMu.Lock()
		sponsorManifest, sponsorManifestFetchedAt = oldManifest, oldFetchedAt
		sponsorManifestMu.Unlock()
	})

	sponsorship := &types.Sponsorship{Org: &types.Org{Name: "Example", LogoDark: "https://cdn.example/sponsors/logo.png"}}
	before := sponsorCardHash(sponsorship)
	sponsorManifestMu.Lock()
	sponsorManifest["logo.png"] = "logo-v2"
	sponsorManifestMu.Unlock()
	after := sponsorCardHash(sponsorship)
	if before == after {
		t.Fatal("sponsor card hash did not change when the Spaces manifest fingerprint changed")
	}
}
