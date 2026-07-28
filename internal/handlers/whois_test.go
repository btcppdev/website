package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestWhoIsProjectDataParticipatesInDirectoryFeatures(t *testing.T) {
	project := &WhoIsProject{
		Project: &types.HackathonProject{
			ID:               "project-id",
			Title:            "Lightning Workshop",
			ShortDescription: "Collaborative channel tooling",
			Tags:             []string{"lightning", "operations"},
		},
		Conf: &types.Conf{Tag: "toronto", Desc: "Toronto"},
	}
	person := &WhoIsPerson{
		PublicID: "builder",
		Speaker:  &types.Speaker{ID: "person-id", Name: "Builder"},
		Projects: []*WhoIsProject{project},
		Editions: []*types.Conf{project.Conf},
	}

	if !whoIsPersonMatches(person, "channel tooling", "lightning", "toronto") {
		t.Fatal("project title, description, tags, and conference should participate in directory filtering")
	}
	if whoIsPersonMatches(person, "unrelated", "", "") {
		t.Fatal("unrelated project query matched")
	}
	talks, projects, editions := whoIsTotals([]*WhoIsPerson{person})
	if talks != 0 || projects != 1 || editions != 1 {
		t.Fatalf("totals = talks %d, projects %d, editions %d", talks, projects, editions)
	}
}

func TestAssignWhoIsProjectMemberPublicIDs(t *testing.T) {
	member := &WhoIsProjectMember{Member: &types.ProjectMember{PersonID: "teammate-id"}}
	people := []*WhoIsPerson{
		{
			PublicID: "owner",
			Speaker:  &types.Speaker{ID: "owner-id"},
			Projects: []*WhoIsProject{{Members: []*WhoIsProjectMember{member}}},
		},
		{PublicID: "teammate", Speaker: &types.Speaker{ID: "teammate-id"}},
	}

	assignWhoIsProjectMemberPublicIDs(people)
	if member.PublicID != "teammate" {
		t.Fatalf("member PublicID = %q, want teammate", member.PublicID)
	}
}
