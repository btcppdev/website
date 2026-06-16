package handlers

import "testing"

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
