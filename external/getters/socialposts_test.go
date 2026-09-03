package getters

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestPostedRefsFromPostsUsesExactConferencePrefix(t *testing.T) {
	posts := []*types.SocialPost{
		{Ref: "dev26-talk-1", Status: "posted"},
		{Ref: "dev260-talk-2", Status: "posted"},
		{Ref: "other-dev26-talk-3", Status: "posted"},
		{Ref: "dev26-talk-4", Status: "failed"},
		{Ref: "dev26-talk-5"},
	}

	got := postedRefsFromPosts(posts, "dev26-")
	if len(got) != 2 || !got["dev26-talk-1"] || !got["dev26-talk-5"] {
		t.Fatalf("posted refs = %#v", got)
	}
}
