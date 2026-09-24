package api

import (
	"encoding/json"
	"testing"

	"btcpp-web/internal/types"
)

func TestRecordingCandidateExposesSavedSocialCard(t *testing.T) {
	for _, card := range []string{"/berlin25/talks/legacy-id-1080p.png", "berlin25/talks/custom.png", ""} {
		t.Run(card, func(t *testing.T) {
			talk := &types.Talk{ID: "new-talk-id", TalkCardURL: card}
			raw, err := json.Marshal(recordingCandidateFromDomain(talk, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			if got, exists := result["social_card"]; !exists || got != card {
				t.Fatalf("social_card = %v, want saved path %q", got, card)
			}
		})
	}
}
