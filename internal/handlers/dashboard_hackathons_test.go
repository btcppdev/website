package handlers

import (
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
)

func TestBuildDashboardHackathonProjects(t *testing.T) {
	projects := buildDashboardHackathonProjects([]*getters.HackathonParticipantProject{
		{
			Project: &types.HackathonProject{
				ID:        "project/id",
				Title:     "Project",
				Status:    getters.ProjectStatusSubmitted,
				ImageURLs: []string{"https://example.com/project.png"},
			},
			Conf:             &types.Conf{Tag: "toronto 2026"},
			CompetitionTitle: "Toronto Hackathon",
			MemberRole:       getters.ProjectMemberRoleOwner,
			TeamSize:         3,
		},
	})
	if len(projects) != 1 {
		t.Fatalf("dashboard projects = %d, want 1", len(projects))
	}
	project := projects[0]
	if project.EditURL != "/toronto%202026/hackathon/projects/project%2Fid/edit" {
		t.Fatalf("edit URL = %q", project.EditURL)
	}
	if project.TeamURL != project.EditURL+"#team" || project.SubmissionURL != project.EditURL+"#submission" {
		t.Fatalf("editor tab URLs = team %q, submission %q", project.TeamURL, project.SubmissionURL)
	}
	if project.StatusLabel != "Submitted" || project.ImageURL != "https://example.com/project.png" {
		t.Fatalf("display fields = status %q, image %q", project.StatusLabel, project.ImageURL)
	}
}

func TestDashboardHackathonProjectStatusLabel(t *testing.T) {
	tests := map[string]string{
		getters.ProjectStatusCreated:   "Draft",
		getters.ProjectStatusSubmitted: "Submitted",
		getters.ProjectStatusAdvanced:  "Advanced",
		getters.ProjectStatusHidden:    "Hidden",
		"":                             "Draft",
	}
	for status, want := range tests {
		if got := dashboardHackathonProjectStatusLabel(status); got != want {
			t.Errorf("status %q = %q, want %q", status, got, want)
		}
	}
}
