package handlers

import (
	"net/http/httptest"
	"testing"

	"btcpp-web/internal/types"
)

func TestWhoIsPageURLPreservesFilters(t *testing.T) {
	r := httptest.NewRequest("GET", "/whois?q=relay&topic=protocol&event=dev26&page=3", nil)
	if got, want := whoIsPageURL(r, 2), "/whois?event=dev26&page=2&q=relay&topic=protocol"; got != want {
		t.Fatalf("page 2 URL = %q, want %q", got, want)
	}
	if got, want := whoIsPageURL(r, 1), "/whois?event=dev26&q=relay&topic=protocol"; got != want {
		t.Fatalf("first-page URL = %q, want %q", got, want)
	}
}

func TestPaginateWhoIsPeopleBoundsLargeDirectory(t *testing.T) {
	people := make([]*WhoIsPerson, 100)
	page, pageNumber, pageCount := paginateWhoIsPeople(people, 3, 48)
	if len(page) != 4 || pageNumber != 3 || pageCount != 3 {
		t.Fatalf("last page = len %d, page %d/%d; want len 4, page 3/3", len(page), pageNumber, pageCount)
	}
	page, pageNumber, pageCount = paginateWhoIsPeople(people, 99, 48)
	if len(page) != 4 || pageNumber != 3 || pageCount != 3 {
		t.Fatalf("clamped page = len %d, page %d/%d; want len 4, page 3/3", len(page), pageNumber, pageCount)
	}
	page, pageNumber, pageCount = paginateWhoIsPeople(nil, 1, 48)
	if len(page) != 0 || pageNumber != 1 || pageCount != 1 {
		t.Fatalf("empty page = len %d, page %d/%d; want len 0, page 1/1", len(page), pageNumber, pageCount)
	}
}

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
