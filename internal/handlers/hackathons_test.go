package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestFilterHackathonCompetitionsSearchesTitleSlugAndConference(t *testing.T) {
	competitions := []*types.HackathonCompetition{
		{ID: "comp-1", ConferenceID: "conf-1", Slug: "berlin-build", Title: "Lightning Builder Day"},
		{ID: "comp-2", ConferenceID: "conf-2", Slug: "austin-ai", Title: "AI Sprint"},
	}
	confs := []*types.Conf{
		{Ref: "conf-1", Tag: "berlin25", Desc: "bitcoin++ Berlin 2025"},
		{Ref: "conf-2", Tag: "austin25", Desc: "bitcoin++ Austin 2025"},
	}

	tests := []struct {
		name string
		q    string
		want string
	}{
		{name: "title", q: "lightning", want: "comp-1"},
		{name: "slug", q: "austin-ai", want: "comp-2"},
		{name: "conference", q: "berlin", want: "comp-1"},
		{name: "conference tag", q: "austin25", want: "comp-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterHackathonCompetitions(competitions, confs, tt.q)
			if len(got) != 1 || got[0].ID != tt.want {
				t.Fatalf("filterHackathonCompetitions(%q) = %#v, want only %s", tt.q, got, tt.want)
			}
		})
	}
}

func TestSortHackathonCompetitions(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	confs := []*types.Conf{
		{Ref: "conf-a", Desc: "bitcoin++ Austin 2025"},
		{Ref: "conf-b", Desc: "bitcoin++ Berlin 2025"},
	}

	tests := []struct {
		name string
		mode string
		want []string
	}{
		{name: "newest", mode: hackathonSortNewest, want: []string{"new", "middle", "old"}},
		{name: "oldest", mode: hackathonSortOldest, want: []string{"old", "middle", "new"}},
		{name: "title", mode: hackathonSortTitle, want: []string{"middle", "old", "new"}},
		{name: "conference", mode: hackathonSortConference, want: []string{"old", "new", "middle"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			competitions := []*types.HackathonCompetition{
				{ID: "new", ConferenceID: "conf-a", Title: "Zebra", CreatedAt: newer},
				{ID: "old", ConferenceID: "conf-a", Title: "Beta", CreatedAt: older},
				{ID: "middle", ConferenceID: "conf-b", Title: "Alpha", CreatedAt: older.Add(24 * time.Hour)},
			}

			sortHackathonCompetitions(competitions, confs, tt.mode)
			for i, want := range tt.want {
				if competitions[i].ID != want {
					t.Fatalf("position %d = %s, want %s", i, competitions[i].ID, want)
				}
			}
		})
	}
}
