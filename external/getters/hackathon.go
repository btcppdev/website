package getters

import (
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

type CompetitionInput struct {
	ConferenceID       string
	Slug               string
	Title              string
	Description        string
	Status             string
	MaxTeamSize        *int
	SubmissionsOpenAt  *time.Time
	SubmissionsCloseAt *time.Time
	PublicGalleryAt    *time.Time
}

type ProjectInput struct {
	CompetitionID     string
	CreatedByPersonID string
	Slug              string
	Title             string
	ShortDescription  string
	Description       string
	GitHubURL         string
	DemoURL           string
	VideoURL          string
	SlidesURL         string
	DocsURL           string
	ProjectNumber     *int
	Tags              []string
}

func CreateCompetition(ctx *config.AppContext, in CompetitionInput) (string, error) {
	if UsePostgresBackend(ctx) {
		return createCompetitionPostgres(ctx, in)
	}
	return "", unsupportedPostgresBackend("hackathon competitions")
}

func GetCompetitionByID(ctx *config.AppContext, competitionID string) (*types.HackathonCompetition, error) {
	if UsePostgresBackend(ctx) {
		return getCompetitionByIDPostgres(ctx, competitionID)
	}
	return nil, unsupportedPostgresBackend("hackathon competitions")
}

func GetCompetitionBySlug(ctx *config.AppContext, slug string) (*types.HackathonCompetition, error) {
	if UsePostgresBackend(ctx) {
		return getCompetitionBySlugPostgres(ctx, slug)
	}
	return nil, unsupportedPostgresBackend("hackathon competitions")
}

func ListCompetitions(ctx *config.AppContext) ([]*types.HackathonCompetition, error) {
	if UsePostgresBackend(ctx) {
		return listCompetitionsPostgres(ctx)
	}
	return nil, unsupportedPostgresBackend("hackathon competitions")
}

func CreateProject(ctx *config.AppContext, in ProjectInput) (string, error) {
	if UsePostgresBackend(ctx) {
		return createProjectPostgres(ctx, in)
	}
	return "", unsupportedPostgresBackend("hackathon projects")
}

func UpdateProject(ctx *config.AppContext, projectID string, in ProjectInput) error {
	if UsePostgresBackend(ctx) {
		return updateProjectPostgres(ctx, projectID, in)
	}
	return unsupportedPostgresBackend("hackathon projects")
}

func SubmitProject(ctx *config.AppContext, projectID string) error {
	if UsePostgresBackend(ctx) {
		return submitProjectPostgres(ctx, projectID)
	}
	return unsupportedPostgresBackend("hackathon projects")
}

func GetProjectByID(ctx *config.AppContext, projectID string) (*types.HackathonProject, error) {
	if UsePostgresBackend(ctx) {
		return getProjectByIDPostgres(ctx, projectID)
	}
	return nil, unsupportedPostgresBackend("hackathon projects")
}

func ListProjectsForCompetition(ctx *config.AppContext, competitionID string, viewer types.HackathonViewer) ([]*types.HackathonProject, error) {
	if UsePostgresBackend(ctx) {
		return listProjectsForCompetitionPostgres(ctx, competitionID, viewer)
	}
	return nil, unsupportedPostgresBackend("hackathon projects")
}

func AddProjectMember(ctx *config.AppContext, projectID, personID, role string) error {
	if UsePostgresBackend(ctx) {
		return addProjectMemberPostgres(ctx, projectID, personID, role)
	}
	return unsupportedPostgresBackend("hackathon project members")
}

func RemoveProjectMember(ctx *config.AppContext, projectID, personID string) error {
	if UsePostgresBackend(ctx) {
		return removeProjectMemberPostgres(ctx, projectID, personID)
	}
	return unsupportedPostgresBackend("hackathon project members")
}

func ListProjectMembers(ctx *config.AppContext, projectID string) ([]*types.ProjectMember, error) {
	if UsePostgresBackend(ctx) {
		return listProjectMembersPostgres(ctx, projectID)
	}
	return nil, unsupportedPostgresBackend("hackathon project members")
}

func CreateProjectInvite(ctx *config.AppContext, projectID, email string, expiresAt *time.Time) (string, *types.ProjectInvite, error) {
	if UsePostgresBackend(ctx) {
		return createProjectInvitePostgres(ctx, projectID, email, expiresAt)
	}
	return "", nil, unsupportedPostgresBackend("hackathon project invites")
}

func AcceptProjectInvite(ctx *config.AppContext, token, personID string) (*types.ProjectInvite, error) {
	if UsePostgresBackend(ctx) {
		return acceptProjectInvitePostgres(ctx, token, personID)
	}
	return nil, unsupportedPostgresBackend("hackathon project invites")
}

func CanViewProject(ctx *config.AppContext, projectID string, viewer types.HackathonViewer) (bool, error) {
	if UsePostgresBackend(ctx) {
		return canViewProjectPostgres(ctx, projectID, viewer)
	}
	return false, unsupportedPostgresBackend("hackathon project visibility")
}
