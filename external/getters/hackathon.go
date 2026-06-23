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
	return createCompetitionPostgres(ctx, in)
}

func UpdateCompetition(ctx *config.AppContext, competitionID string, in CompetitionInput) error {
	return updateCompetitionPostgres(ctx, competitionID, in)
}

func UpdateCompetitionVisibility(ctx *config.AppContext, competitionID, visibility string) error {
	return updateCompetitionVisibilityPostgres(ctx, competitionID, visibility)
}

func GetCompetitionByID(ctx *config.AppContext, competitionID string) (*types.HackathonCompetition, error) {
	return getCompetitionByIDPostgres(ctx, competitionID)
}

func GetCompetitionBySlug(ctx *config.AppContext, slug string) (*types.HackathonCompetition, error) {
	return getCompetitionBySlugPostgres(ctx, slug)
}

func ListCompetitions(ctx *config.AppContext) ([]*types.HackathonCompetition, error) {
	return listCompetitionsPostgres(ctx)
}

func CreateProject(ctx *config.AppContext, in ProjectInput) (string, error) {
	return createProjectPostgres(ctx, in)
}

func UpdateProject(ctx *config.AppContext, projectID string, in ProjectInput) error {
	return updateProjectPostgres(ctx, projectID, in)
}

func SubmitProject(ctx *config.AppContext, projectID string) error {
	return submitProjectPostgres(ctx, projectID)
}

func SetProjectAwardOptIns(ctx *config.AppContext, projectID string, awardIDs []string) error {
	return setProjectAwardOptInsPostgres(ctx, projectID, awardIDs)
}

func ListProjectAwardOptInsForProject(ctx *config.AppContext, projectID string) ([]*types.ProjectAwardOptIn, error) {
	return listProjectAwardOptInsForProjectPostgres(ctx, projectID)
}

func ListProjectAwardOptInsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.ProjectAwardOptIn, error) {
	return listProjectAwardOptInsForCompetitionPostgres(ctx, competitionID)
}

func UpdateProjectAdminFields(ctx *config.AppContext, competitionID, projectID, status string, projectNumber *int) error {
	return updateProjectAdminFieldsPostgres(ctx, competitionID, projectID, status, projectNumber)
}

func AssignMissingProjectNumbers(ctx *config.AppContext, competitionID string) (int, error) {
	return assignMissingProjectNumbersPostgres(ctx, competitionID)
}

func GetProjectByID(ctx *config.AppContext, projectID string) (*types.HackathonProject, error) {
	return getProjectByIDPostgres(ctx, projectID)
}

func ListProjectsForCompetition(ctx *config.AppContext, competitionID string, viewer types.HackathonViewer) ([]*types.HackathonProject, error) {
	return listProjectsForCompetitionPostgres(ctx, competitionID, viewer)
}

func AddProjectMember(ctx *config.AppContext, projectID, personID, role string) error {
	return addProjectMemberPostgres(ctx, projectID, personID, role)
}

func RemoveProjectMember(ctx *config.AppContext, projectID, personID string) error {
	return removeProjectMemberPostgres(ctx, projectID, personID)
}

func ListProjectMembers(ctx *config.AppContext, projectID string) ([]*types.ProjectMember, error) {
	return listProjectMembersPostgres(ctx, projectID)
}

func GetPersonIDByEmail(ctx *config.AppContext, email string) (string, error) {
	return getPersonIDByEmailPostgres(ctx, email)
}

func CreateProjectInvite(ctx *config.AppContext, projectID, email string, expiresAt *time.Time) (string, *types.ProjectInvite, error) {
	return createProjectInvitePostgres(ctx, projectID, email, expiresAt)
}

func AcceptProjectInvite(ctx *config.AppContext, token, personID string) (*types.ProjectInvite, error) {
	return acceptProjectInvitePostgres(ctx, token, personID)
}

func CanViewProject(ctx *config.AppContext, projectID string, viewer types.HackathonViewer) (bool, error) {
	return canViewProjectPostgres(ctx, projectID, viewer)
}

func CreateJudgeEvent(ctx *config.AppContext, in JudgeEventInput) (string, error) {
	return createJudgeEventPostgres(ctx, in)
}

func ListJudgeEvents(ctx *config.AppContext, competitionID string) ([]*types.JudgeEvent, error) {
	return listJudgeEventsPostgres(ctx, competitionID)
}

func DeleteJudgeEvent(ctx *config.AppContext, competitionID, judgeEventID string) error {
	return deleteJudgeEventPostgres(ctx, competitionID, judgeEventID)
}

func AddCompetitionJudge(ctx *config.AppContext, competitionID, personID, judgeType string) error {
	return addCompetitionJudgePostgres(ctx, competitionID, personID, judgeType)
}

func RemoveCompetitionJudge(ctx *config.AppContext, competitionID, personID, judgeType string) error {
	return removeCompetitionJudgePostgres(ctx, competitionID, personID, judgeType)
}

func ListCompetitionJudges(ctx *config.AppContext, competitionID string) ([]*types.CompetitionJudge, error) {
	return listCompetitionJudgesPostgres(ctx, competitionID)
}

func UpsertScorecard(ctx *config.AppContext, in ScorecardInput) (*types.Scorecard, error) {
	return upsertScorecardPostgres(ctx, in)
}

func ListScorecardsForJudge(ctx *config.AppContext, competitionID, judgePersonID string) ([]*types.Scorecard, error) {
	return listScorecardsForJudgePostgres(ctx, competitionID, judgePersonID)
}

func ListScorecardsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.Scorecard, error) {
	return listScorecardsForCompetitionPostgres(ctx, competitionID)
}

func CreateAward(ctx *config.AppContext, in AwardInput) (string, error) {
	return createAwardPostgres(ctx, in)
}

func UpdateAward(ctx *config.AppContext, awardID string, in AwardInput) error {
	return updateAwardPostgres(ctx, awardID, in)
}

func ArchiveAward(ctx *config.AppContext, competitionID, awardID string) error {
	return archiveAwardPostgres(ctx, competitionID, awardID)
}

func RestoreAward(ctx *config.AppContext, competitionID, awardID string) error {
	return restoreAwardPostgres(ctx, competitionID, awardID)
}

func DeleteArchivedAward(ctx *config.AppContext, competitionID, awardID string) error {
	return deleteArchivedAwardPostgres(ctx, competitionID, awardID)
}

func ListAwardsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.Award, error) {
	return listAwardsForCompetitionPostgres(ctx, competitionID)
}

func ListArchivedAwardsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.Award, error) {
	return listArchivedAwardsForCompetitionPostgres(ctx, competitionID)
}

func CreatePrize(ctx *config.AppContext, in PrizeInput) (string, error) {
	return createPrizePostgres(ctx, in)
}

func ListPrizesForCompetition(ctx *config.AppContext, competitionID string) ([]*types.Prize, error) {
	return listPrizesForCompetitionPostgres(ctx, competitionID)
}

func AssignProjectAward(ctx *config.AppContext, awardID, projectID string) error {
	return assignProjectAwardPostgres(ctx, awardID, projectID)
}

func RemoveProjectAward(ctx *config.AppContext, awardID, projectID string) error {
	return removeProjectAwardPostgres(ctx, awardID, projectID)
}

func ListProjectAwardsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.ProjectAward, error) {
	return listProjectAwardsForCompetitionPostgres(ctx, competitionID)
}
