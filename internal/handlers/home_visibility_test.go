package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

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

func confTags(confs []*types.Conf) []string {
	out := make([]string, 0, len(confs))
	for _, conf := range confs {
		if conf != nil {
			out = append(out, conf.Tag)
		}
	}
	return out
}
