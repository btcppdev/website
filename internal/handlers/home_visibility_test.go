package handlers

import (
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
)

func TestRepeatHomepageArchiveAssetsCapsAndPadsToCount(t *testing.T) {
	many := make([]*getters.HomepageArchiveAsset, 30)
	for i := range many {
		many[i] = &getters.HomepageArchiveAsset{Image: string(rune('a' + i))}
	}
	if got := repeatHomepageArchiveAssets(many, 24); len(got) != 24 {
		t.Fatalf("capped archive assets = %d, want 24", len(got))
	}

	few := []*getters.HomepageArchiveAsset{{Image: "one"}, {Image: "two"}}
	got := repeatHomepageArchiveAssets(few, 5)
	if len(got) != 5 || got[2].Image != "one" || got[4].Image != "one" {
		t.Fatalf("padded archive assets = %#v", got)
	}
}

func TestHomeConferenceListsUsePublicationStatus(t *testing.T) {
	now := time.Now()
	draftFuture := &types.Conf{
		Tag:               "madeira",
		Desc:              "Madeira",
		PublicationStatus: "draft",
		StartDate:         now.AddDate(0, 2, 0),
		EndDate:           now.AddDate(0, 2, 2),
		MapXPercent:       10,
		MapYPercent:       20,
	}
	publishedFuture := &types.Conf{
		Tag:               "berlin",
		Desc:              "Berlin",
		PublicationStatus: "published",
		StartDate:         now.AddDate(0, 1, 0),
		EndDate:           now.AddDate(0, 1, 2),
		MapXPercent:       30,
		MapYPercent:       40,
	}
	publishedPast := &types.Conf{
		Tag:               "durham",
		Desc:              "Durham",
		PublicationStatus: "published",
		StartDate:         now.AddDate(0, -2, 0),
		EndDate:           now.AddDate(0, -2, 1),
		MapXPercent:       50,
		MapYPercent:       60,
	}
	confs := []*types.Conf{draftFuture, publishedFuture, publishedPast}

	if got := homeUpcomingConfs(confs); len(got) != 1 || got[0].Tag != "berlin" {
		t.Fatalf("homeUpcomingConfs = %v, want only berlin", confTags(got))
	}
	if got := homePastConfs(confs); len(got) != 1 || got[0].Tag != "durham" {
		t.Fatalf("homePastConfs = %v, want only durham", confTags(got))
	}
	years := homeTimelineYears(confs)
	for _, year := range years {
		for _, conf := range year.Confs {
			if conf.Tag == "madeira" {
				t.Fatalf("homeTimelineYears included draft conf %q", conf.Tag)
			}
		}
	}
	if got := homeMapMarkers(confs); len(got) != 2 {
		t.Fatalf("homeMapMarkers count = %d, want 2 published markers", len(got))
	}
}

func TestHomeConferenceListsHonorPostEventGrace(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatalf("load Toronto timezone: %s", err)
	}
	conf := &types.Conf{
		Tag:               "toronto",
		PublicationStatus: "published",
		Timezone:          "America/Toronto",
		TZ:                loc,
		StartDate:         time.Date(2026, time.July, 22, 0, 0, 0, 0, loc),
		EndDate:           time.Date(2026, time.July, 24, 0, 0, 0, 0, loc),
	}

	duringGrace := time.Date(2026, time.July, 26, 11, 59, 0, 0, loc)
	if got := homeUpcomingConfsAt([]*types.Conf{conf}, duringGrace); len(got) != 1 {
		t.Fatalf("upcoming during grace = %v, want Toronto", confTags(got))
	}
	if got := homePastConfsAt([]*types.Conf{conf}, duringGrace); len(got) != 0 {
		t.Fatalf("past during grace = %v, want none", confTags(got))
	}

	afterGrace := time.Date(2026, time.July, 26, 12, 0, 0, 0, loc)
	if got := homeUpcomingConfsAt([]*types.Conf{conf}, afterGrace); len(got) != 0 {
		t.Fatalf("upcoming after grace = %v, want none", confTags(got))
	}
	if got := homePastConfsAt([]*types.Conf{conf}, afterGrace); len(got) != 1 {
		t.Fatalf("past after grace = %v, want Toronto", confTags(got))
	}
}

func confTags(confs []*types.Conf) []string {
	out := make([]string, 0, len(confs))
	for _, conf := range confs {
		if conf != nil {
			out = append(out, conf.Tag)
		}
	}
	return out
}
