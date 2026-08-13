package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestProfileHandleForSlugRequiresMatchingURLHost(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "github URL", raw: "https://github.com/Jestopher-BTC", want: "Jestopher-BTC"},
		{name: "www github URL", raw: "https://www.github.com/Jestopher-BTC/", want: "Jestopher-BTC"},
		{name: "github path", raw: "github.com/Jestopher-BTC", want: "Jestopher-BTC"},
		{name: "bare handle", raw: "@Jestopher-BTC", want: "Jestopher-BTC"},
		{name: "other URL", raw: "https://amboss.space", want: ""},
		{name: "bare domain", raw: "amboss.space", want: ""},
		{name: "empty github URL", raw: "https://github.com/", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileHandleForSlug(tt.raw, "github.com"); got != tt.want {
				t.Fatalf("profileHandleForSlug(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestPublicSpeakerSlugFallsBackAfterInvalidGithubURL(t *testing.T) {
	speaker := &types.Speaker{
		Name:    "Jesse Shrader",
		Github:  "https://amboss.space",
		Twitter: types.ParseTwitter("Jestopher_BTC"),
	}
	if got, want := publicSpeakerSlug(speaker), "jestopher-btc"; got != want {
		t.Fatalf("publicSpeakerSlug() = %q, want %q", got, want)
	}
}

func TestAssignWhoIsPublicIDsSuffixesEveryCollision(t *testing.T) {
	people := []*WhoIsPerson{
		{Speaker: &types.Speaker{ID: "aaaaaaaa-1111-2222-3333-444444444444", Name: "Alex One"}},
		{Speaker: &types.Speaker{ID: "bbbbbbbb-1111-2222-3333-444444444444", Name: "Alex Two"}},
		{Speaker: &types.Speaker{ID: "cccccccc-1111-2222-3333-444444444444", Name: "Unique Person"}},
	}
	people[0].Speaker.Twitter = types.ParseTwitter("shared")
	people[1].Speaker.Twitter = types.ParseTwitter("shared")

	publicIDs := assignWhoIsPublicIDs(people)
	wants := map[string]string{
		people[0].Speaker.ID: "shared-aaaaaaaa",
		people[1].Speaker.ID: "shared-bbbbbbbb",
		people[2].Speaker.ID: "unique-person",
	}
	for id, want := range wants {
		if got := publicIDs[id]; got != want {
			t.Errorf("public ID for %s = %q, want %q", id, got, want)
		}
	}
}

func TestWhoIsPublicPathFallsBackToNameSearch(t *testing.T) {
	speaker := &types.Speaker{Name: "Jesse Shrader"}
	if got, want := whoIsPublicPath(nil, speaker), "/whois?q=Jesse+Shrader"; got != want {
		t.Fatalf("whoIsPublicPath() = %q, want %q", got, want)
	}
}
