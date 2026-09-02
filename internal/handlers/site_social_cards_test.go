package handlers

import (
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"
)

func TestSiteSocialCardPathIncludesContentVersion(t *testing.T) {
	card := imgproc.SiteSocialCard{Kind: "person", Title: "Mara Chen"}
	got := siteSocialCardPath("person", "mara-chen", card)
	if !strings.HasPrefix(got, "/social-cards/person/mara-chen.jpg?v=") {
		t.Fatalf("siteSocialCardPath = %q", got)
	}
	changed := card
	changed.Title = "Mara Nakamoto"
	if got == siteSocialCardPath("person", "mara-chen", changed) {
		t.Fatal("site social card URL did not change with card content")
	}
}

func TestHomeSocialCardFeaturesNextConference(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	confs := []*types.Conf{{Tag: "dev26", PublicationStatus: "published", Desc: "bitcoin++ Austin", Tagline: "The local edition", DateDesc: "August 2026", Location: "Austin"}}
	card := homeSocialCard(ctx, confs)
	if card.Title != "bitcoin++ Austin" {
		t.Fatalf("home card title = %q", card.Title)
	}
	if !strings.Contains(card.Subtitle, "Austin") {
		t.Fatalf("home card subtitle = %q", card.Subtitle)
	}
}

func TestPersonSocialCardIncludesTalkAndAward(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	person := &WhoIsPerson{
		PublicID: "mara-chen",
		Speaker:  &types.Speaker{Name: "Mara Chen"},
		Talks:    []*WhoIsTalk{{Talk: &types.Talk{Name: "Building Bitcoin"}}},
		Projects: []*WhoIsProject{{Project: &types.HackathonProject{Title: "Relay Lab"}, Awards: []*getters.PublicProfileProjectAward{{Title: "Best in Show"}}}},
	}
	card := personSocialCard(ctx, person)
	if !strings.Contains(card.Subtitle, "Talk: Building Bitcoin") || !strings.Contains(card.Subtitle, "Award: Best in Show") {
		t.Fatalf("person card subtitle = %q", card.Subtitle)
	}
}

func TestSiteSocialCardImageHostAllowlist(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	if !siteSocialCardImageHostAllowed(ctx, "http://localhost:8888/static/card.jpg") {
		t.Fatal("development site image was rejected")
	}
	if siteSocialCardImageHostAllowed(ctx, "http://169.254.169.254/latest/meta-data") {
		t.Fatal("cloud metadata host was accepted")
	}
	if siteSocialCardImageHostAllowed(ctx, "http://example.test/user-controlled.jpg") {
		t.Fatal("untrusted image host was accepted")
	}
}
