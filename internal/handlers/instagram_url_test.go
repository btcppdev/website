package handlers

import "testing"

func TestInstagramURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: " ", want: ""},
		{name: "handle", raw: "alice", want: "https://www.instagram.com/alice"},
		{name: "at handle", raw: "@alice", want: "https://www.instagram.com/alice"},
		{name: "pathless domain", raw: "instagram.com/alice", want: "https://www.instagram.com/alice"},
		{name: "www domain", raw: "www.instagram.com/alice", want: "https://www.instagram.com/alice"},
		{name: "https url", raw: "https://instagram.com/alice", want: "https://instagram.com/alice"},
		{name: "http url", raw: "http://instagram.com/alice", want: "http://instagram.com/alice"},
		{name: "path with trailing slash", raw: "instagram.com/alice/", want: "https://www.instagram.com/alice/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := instagramURL(tt.raw); got != tt.want {
				t.Fatalf("instagramURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
