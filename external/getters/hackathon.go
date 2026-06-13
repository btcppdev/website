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
	Visibility         string
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

type JudgeEventInput struct {
	CompetitionID         string
	Name                  string
	PlaybookType          string
	Ordering              int
	StartsAt              *time.Time
	EndsAt                *time.Time
	StartingProjectNumber *int
}

type ScorecardInput struct {
	JudgeEventID   string
	ProjectID      string
	JudgePersonID  string
	IdeaScore      *int
	ExecutionScore *int
	ImpactScore    *int
	Rank           *int
	NoShow         bool
	Comments       string
}

type AwardInput struct {
	CompetitionID string
	Title         string
	Description   string
	PhotoURL      string
	MaxAwardees   *int
	OptInRequired bool
	Status        string
}

type PrizeInput struct {
	AwardID        string
	PrizeType      string
	Title          string
	Description    string
	ValueText      string
	PoolPercentage *float64
	PoolURL        string
	Status         string
	Comments       string
}

func CreateCompetition(ctx *config.AppContext, in CompetitionInput) (string, error) {
	if UsePostgresBackend(ctx) {
		return createCompetitionPostgres(ctx, in)
	}
	return "", unsupportedPostgresBackend("hackathon competitions")
}

func UpdateCompetition(ctx *config.AppContext, competitionID string, in CompetitionInput) error {
	if UsePostgresBackend(ctx) {
		return updateCompetitionPostgres(ctx, competitionID, in)
	}
	return unsupportedPostgresBackend("hackathon competitions")
}

func UpdateCompetitionVisibility(ctx *config.AppContext, competitionID, visibility string) error {
	if UsePostgresBackend(ctx) {
		return updateCompetitionVisibilityPostgres(ctx, competitionID, visibility)
	}
	return unsupportedPostgresBackend("hackathon competition visibility")
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

func GetPersonIDByEmail(ctx *config.AppContext, email string) (string, error) {
	if UsePostgresBackend(ctx) {
		return getPersonIDByEmailPostgres(ctx, email)
	}
	return "", unsupportedPostgresBackend("people")
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

func CreateJudgeEvent(ctx *config.AppContext, in JudgeEventInput) (string, error) {
	if UsePostgresBackend(ctx) {
		return createJudgeEventPostgres(ctx, in)
	}
	return "", unsupportedPostgresBackend("hackathon judge events")
}

func ListJudgeEvents(ctx *config.AppContext, competitionID string) ([]*types.JudgeEvent, error) {
	if UsePostgresBackend(ctx) {
		return listJudgeEventsPostgres(ctx, competitionID)
	}
	return nil, unsupportedPostgresBackend("hackathon judge events")
}

func AddCompetitionJudge(ctx *config.AppContext, competitionID, personID, judgeType string) error {
	if UsePostgresBackend(ctx) {
		return addCompetitionJudgePostgres(ctx, competitionID, personID, judgeType)
	}
	return unsupportedPostgresBackend("hackathon judges")
}

func RemoveCompetitionJudge(ctx *config.AppContext, competitionID, personID, judgeType string) error {
	if UsePostgresBackend(ctx) {
		return removeCompetitionJudgePostgres(ctx, competitionID, personID, judgeType)
	}
	return unsupportedPostgresBackend("hackathon judges")
}

func ListCompetitionJudges(ctx *config.AppContext, competitionID string) ([]*types.CompetitionJudge, error) {
	if UsePostgresBackend(ctx) {
		return listCompetitionJudgesPostgres(ctx, competitionID)
	}
	return nil, unsupportedPostgresBackend("hackathon judges")
}

func UpsertScorecard(ctx *config.AppContext, in ScorecardInput) (*types.Scorecard, error) {
	if UsePostgresBackend(ctx) {
		return upsertScorecardPostgres(ctx, in)
	}
	return nil, unsupportedPostgresBackend("hackathon scorecards")
}

func ListScorecardsForJudge(ctx *config.AppContext, competitionID, judgePersonID string) ([]*types.Scorecard, error) {
	if UsePostgresBackend(ctx) {
		return listScorecardsForJudgePostgres(ctx, competitionID, judgePersonID)
	}
	return nil, unsupportedPostgresBackend("hackathon scorecards")
}

func ListScorecardsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.Scorecard, error) {
	if UsePostgresBackend(ctx) {
		return listScorecardsForCompetitionPostgres(ctx, competitionID)
	}
	return nil, unsupportedPostgresBackend("hackathon scorecards")
}

func CreateAward(ctx *config.AppContext, in AwardInput) (string, error) {
	if UsePostgresBackend(ctx) {
		return createAwardPostgres(ctx, in)
	}
	return "", unsupportedPostgresBackend("hackathon awards")
}

func ListAwardsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.Award, error) {
	if UsePostgresBackend(ctx) {
		return listAwardsForCompetitionPostgres(ctx, competitionID)
	}
	return nil, unsupportedPostgresBackend("hackathon awards")
}

func CreatePrize(ctx *config.AppContext, in PrizeInput) (string, error) {
	if UsePostgresBackend(ctx) {
		return createPrizePostgres(ctx, in)
	}
	return "", unsupportedPostgresBackend("hackathon prizes")
}

func ListPrizesForCompetition(ctx *config.AppContext, competitionID string) ([]*types.Prize, error) {
	if UsePostgresBackend(ctx) {
		return listPrizesForCompetitionPostgres(ctx, competitionID)
	}
	return nil, unsupportedPostgresBackend("hackathon prizes")
}

func AssignProjectAward(ctx *config.AppContext, awardID, projectID string) error {
	if UsePostgresBackend(ctx) {
		return assignProjectAwardPostgres(ctx, awardID, projectID)
	}
	return unsupportedPostgresBackend("hackathon project awards")
}

func RemoveProjectAward(ctx *config.AppContext, awardID, projectID string) error {
	if UsePostgresBackend(ctx) {
		return removeProjectAwardPostgres(ctx, awardID, projectID)
	}
	return unsupportedPostgresBackend("hackathon project awards")
}

func ListProjectAwardsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.ProjectAward, error) {
	if UsePostgresBackend(ctx) {
		return listProjectAwardsForCompetitionPostgres(ctx, competitionID)
	}
	return nil, unsupportedPostgresBackend("hackathon project awards")
}
