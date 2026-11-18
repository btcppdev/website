package handlers

import (
	"btcpp-web/internal/types"
	"testing"
)

func TestLegacySocialPostPreventsDuplicateAfterCutover(t *testing.T) {
	for _, tc := range []struct {
		name    string
		post    *types.SocialPost
		blocked bool
	}{
		{"new post", nil, false},
		{"pending old delivery", &types.SocialPost{PostedTo: "buffer", Status: "queued"}, true},
		{"ambiguous old result", &types.SocialPost{PostedTo: "buffer", Status: "failed"}, true},
		{"published old delivery", &types.SocialPost{PostedTo: "buffer-twitter", Status: "posted"}, true},
		{"unknown old state", &types.SocialPost{PostedTo: "buffer"}, true},
		{"canceled old delivery", &types.SocialPost{PostedTo: "buffer", Status: " CANCELED "}, false},
		{"cancelled old delivery", &types.SocialPost{PostedTo: "buffer-twitter", Status: "cancelled"}, false},
		{"retry existing local queue", &types.SocialPost{PostedTo: "publishing", Status: "queued"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := legacySocialPostBlocks(tc.post); got != tc.blocked {
				t.Fatalf("blocked=%v, want %v", got, tc.blocked)
			}
		})
	}
}
