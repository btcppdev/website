package getters

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func projectIsPublicPostgres(ctx *config.AppContext, project *types.HackathonProject) bool {
	if project == nil {
		return false
	}
	if project.Status == ProjectStatusCreated || project.Status == ProjectStatusHidden {
		return false
	}
	var publicGalleryEnabled bool
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT public_gallery_enabled
		FROM competitions
		WHERE id = $1
	`, project.CompetitionID).Scan(&publicGalleryEnabled); err != nil {
		return false
	}
	return publicGalleryEnabled
}

func scanCompetition(rows pgx.Rows) (*types.HackathonCompetition, error) {
	var competition types.HackathonCompetition
	var maxTeamSize sql.NullInt64
	var submissionsOpenAt, submissionsCloseAt, publicGalleryAt pgtype.Timestamptz
	var hackingStartsAt, hackingEndsAt, judgesMeetingAt pgtype.Timestamptz
	var expoStartsAt, expoEndsAt, expoJudgingStartsAt, expoJudgingEndsAt pgtype.Timestamptz
	var finalsStartsAt, finalsEndsAt, finalsJudgingStartsAt, finalsJudgingEndsAt pgtype.Timestamptz
	var awardsCeremonyAt, resultsFinalizedAt pgtype.Timestamptz
	if err := rows.Scan(
		&competition.ID,
		&competition.ConferenceID,
		&competition.Title,
		&competition.Description,
		&competition.DescriptionFormat,
		&competition.Visibility,
		&competition.LifecycleOverride,
		&competition.JudgingMode,
		&competition.PublicGalleryEnabled,
		&competition.AllowLateSubmissions,
		&competition.PublicTablesEnabled,
		&maxTeamSize,
		&submissionsOpenAt,
		&submissionsCloseAt,
		&publicGalleryAt,
		&hackingStartsAt,
		&hackingEndsAt,
		&judgesMeetingAt,
		&expoStartsAt,
		&expoEndsAt,
		&expoJudgingStartsAt,
		&expoJudgingEndsAt,
		&finalsStartsAt,
		&finalsEndsAt,
		&finalsJudgingStartsAt,
		&finalsJudgingEndsAt,
		&awardsCeremonyAt,
		&resultsFinalizedAt,
		&competition.ResultsFinalizedBy,
		&competition.ResultsFinalizedName,
		&competition.CreatedAt,
		&competition.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if maxTeamSize.Valid {
		n := int(maxTeamSize.Int64)
		competition.MaxTeamSize = &n
	}
	competition.SubmissionsOpenAt = pgTimePtr(submissionsOpenAt)
	competition.SubmissionsCloseAt = pgTimePtr(submissionsCloseAt)
	competition.PublicGalleryAt = pgTimePtr(publicGalleryAt)
	competition.Visibility = normalizeCompetitionVisibility(competition.Visibility)
	competition.LifecycleOverride = normalizeCompetitionLifecycleOverride(competition.LifecycleOverride)
	competition.JudgingMode = normalizeCompetitionJudgingMode(competition.JudgingMode)
	competition.HackingStartsAt = pgTimePtr(hackingStartsAt)
	competition.HackingEndsAt = pgTimePtr(hackingEndsAt)
	competition.JudgesMeetingAt = pgTimePtr(judgesMeetingAt)
	competition.ExpoStartsAt = pgTimePtr(expoStartsAt)
	competition.ExpoEndsAt = pgTimePtr(expoEndsAt)
	competition.ExpoJudgingStartsAt = pgTimePtr(expoJudgingStartsAt)
	competition.ExpoJudgingEndsAt = pgTimePtr(expoJudgingEndsAt)
	competition.FinalsStartsAt = pgTimePtr(finalsStartsAt)
	competition.FinalsEndsAt = pgTimePtr(finalsEndsAt)
	competition.FinalsJudgingStartsAt = pgTimePtr(finalsJudgingStartsAt)
	competition.FinalsJudgingEndsAt = pgTimePtr(finalsJudgingEndsAt)
	competition.AwardsCeremonyAt = pgTimePtr(awardsCeremonyAt)
	competition.ResultsFinalizedAt = pgTimePtr(resultsFinalizedAt)
	return &competition, nil
}

func scanProject(rows pgx.Rows) (*types.HackathonProject, error) {
	var project types.HackathonProject
	var projectNumber sql.NullInt64
	var submittedAt pgtype.Timestamptz
	if err := rows.Scan(
		&project.ID,
		&project.CompetitionID,
		&project.CreatedByPersonID,
		&project.Slug,
		&project.Title,
		&project.ShortDescription,
		&project.Description,
		&project.DescriptionFormat,
		&project.ImageURL,
		&project.ImageURLs,
		&project.GitHubURL,
		&project.DemoURL,
		&project.VideoURL,
		&project.SlidesURL,
		&project.DocsURL,
		&projectNumber,
		&project.Status,
		&project.Tags,
		&submittedAt,
		&project.CreatedAt,
		&project.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if projectNumber.Valid {
		n := int(projectNumber.Int64)
		project.ProjectNumber = &n
	}
	project.Status = normalizeProjectStatus(project.Status)
	project.SubmittedAt = pgTimePtr(submittedAt)
	return &project, nil
}

type pgScanner interface {
	Scan(dest ...any) error
}

func scanCompetitionScheduleSegment(row pgScanner) (*types.CompetitionScheduleSegment, error) {
	var segment types.CompetitionScheduleSegment
	if err := row.Scan(
		&segment.ID,
		&segment.CompetitionID,
		&segment.ProposalID,
		&segment.ConfTalkID,
		&segment.SegmentType,
		&segment.Title,
		&segment.DefaultDurationMinutes,
		&segment.Ordering,
		&segment.CreatedAt,
		&segment.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &segment, nil
}

func scanJudgeEvent(rows pgx.Rows) (*types.JudgeEvent, error) {
	var event types.JudgeEvent
	var startsAt, endsAt pgtype.Timestamptz
	var startingProjectNumber sql.NullInt64
	if err := rows.Scan(
		&event.ID,
		&event.CompetitionID,
		&event.ScheduleSegmentID,
		&event.Name,
		&event.PlaybookType,
		&event.State,
		&event.Ordering,
		&startsAt,
		&endsAt,
		&startingProjectNumber,
		&event.RankLimit,
		&event.CreatedAt,
		&event.UpdatedAt,
	); err != nil {
		return nil, err
	}
	event.StartsAt = pgTimePtr(startsAt)
	event.EndsAt = pgTimePtr(endsAt)
	event.State = normalizeJudgeEventState(event.State)
	if startingProjectNumber.Valid {
		n := int(startingProjectNumber.Int64)
		event.StartingProjectNumber = &n
	}
	event.PlaybookType = normalizeJudgeType(event.PlaybookType)
	if event.RankLimit <= 0 {
		event.RankLimit = 4
	}
	return &event, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanScorecard(row scanner) (*types.Scorecard, error) {
	var scorecard types.Scorecard
	var rank sql.NullInt64
	var submittedAt pgtype.Timestamptz
	if err := row.Scan(
		&scorecard.ID,
		&scorecard.JudgeEventID,
		&scorecard.ProjectID,
		&scorecard.JudgePersonID,
		&rank,
		&scorecard.Comments,
		&submittedAt,
		&scorecard.CreatedAt,
		&scorecard.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if rank.Valid {
		n := int(rank.Int64)
		scorecard.Rank = &n
	}
	scorecard.SubmittedAt = pgTimePtr(submittedAt)
	return &scorecard, nil
}

func scanAward(rows pgx.Rows) (*types.Award, error) {
	var award types.Award
	var awardRank sql.NullInt64
	var maxAwardees sql.NullInt64
	var archivedAt pgtype.Timestamptz
	if err := rows.Scan(
		&award.ID,
		&award.CompetitionID,
		&award.SponsoredByOrgID,
		&award.AwardType,
		&award.Title,
		&award.Description,
		&award.JudgingInstructions,
		&awardRank,
		&maxAwardees,
		&award.OptInRequired,
		&award.FinalistsOnly,
		&award.Status,
		&award.CreatedAt,
		&award.UpdatedAt,
		&archivedAt,
	); err != nil {
		return nil, err
	}
	if maxAwardees.Valid {
		n := int(maxAwardees.Int64)
		award.MaxAwardees = &n
	}
	if awardRank.Valid {
		n := int(awardRank.Int64)
		award.AwardRank = &n
	}
	award.AwardType = normalizeAwardType(award.AwardType)
	award.Status = normalizeAwardStatus(award.Status)
	award.ArchivedAt = pgTimePtr(archivedAt)
	return &award, nil
}

func scanAwardJudge(rows pgx.Rows) (*types.AwardJudge, error) {
	var judge types.AwardJudge
	if err := rows.Scan(&judge.AwardID, &judge.PersonID, &judge.Name, &judge.Email, &judge.Photo, &judge.CreatedAt); err != nil {
		return nil, err
	}
	return &judge, nil
}

func scanPrize(rows pgx.Rows) (*types.Prize, error) {
	var prize types.Prize
	var poolPercentage pgtype.Numeric
	if err := rows.Scan(
		&prize.ID,
		&prize.AwardID,
		&prize.PrizeType,
		&prize.Title,
		&prize.Description,
		&prize.ValueText,
		&poolPercentage,
		&prize.PoolURL,
		&prize.Status,
		&prize.Comments,
		&prize.CreatedAt,
		&prize.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if poolPercentage.Valid {
		value, err := poolPercentage.Float64Value()
		if err != nil {
			return nil, err
		}
		n := value.Float64
		prize.PoolPercentage = &n
	}
	prize.PrizeType = normalizePrizeType(prize.PrizeType)
	prize.Status = normalizePrizeStatus(prize.Status)
	return &prize, nil
}

func scanProjectAward(rows pgx.Rows) (*types.ProjectAward, error) {
	var award types.ProjectAward
	var projectNumber sql.NullInt64
	if err := rows.Scan(
		&award.ProjectID,
		&award.AwardID,
		&award.ProjectTitle,
		&projectNumber,
		&award.AwardedAt,
	); err != nil {
		return nil, err
	}
	if projectNumber.Valid {
		n := int(projectNumber.Int64)
		award.ProjectNumber = &n
	}
	return &award, nil
}

func scanProjectAwardOptIns(rows pgx.Rows, label string) ([]*types.ProjectAwardOptIn, error) {
	var out []*types.ProjectAwardOptIn
	for rows.Next() {
		optIn, err := scanProjectAwardOptIn(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project award opt-in: %w", err)
		}
		out = append(out, optIn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project award opt-ins for %s: %w", label, err)
	}
	return out, nil
}

func scanProjectAwardOptIn(rows pgx.Rows) (*types.ProjectAwardOptIn, error) {
	var optIn types.ProjectAwardOptIn
	var projectNumber sql.NullInt64
	if err := rows.Scan(
		&optIn.ProjectID,
		&optIn.AwardID,
		&optIn.ProjectTitle,
		&projectNumber,
		&optIn.AwardTitle,
		&optIn.OptedInAt,
	); err != nil {
		return nil, err
	}
	if projectNumber.Valid {
		n := int(projectNumber.Int64)
		optIn.ProjectNumber = &n
	}
	return &optIn, nil
}

func normalizedUniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func pgTimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func normalizeCompetitionInput(in CompetitionInput) CompetitionInput {
	in.ConferenceID = strings.TrimSpace(in.ConferenceID)
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.DescriptionFormat = normalizeCompetitionDescriptionFormat(in.DescriptionFormat)
	in.Visibility = normalizeCompetitionVisibility(in.Visibility)
	in.LifecycleOverride = normalizeCompetitionLifecycleOverride(in.LifecycleOverride)
	in.JudgingMode = normalizeCompetitionJudgingMode(in.JudgingMode)
	return in
}

func normalizeCompetitionDescriptionFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CompetitionDescriptionFormatPlain:
		return CompetitionDescriptionFormatPlain
	case CompetitionDescriptionFormatMarkdown:
		return CompetitionDescriptionFormatMarkdown
	case CompetitionDescriptionFormatHTML:
		return CompetitionDescriptionFormatHTML
	case "":
		return CompetitionDescriptionFormatMarkdown
	default:
		return ""
	}
}

func normalizeCompetitionVisibility(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "draft", "hidden":
		return CompetitionVisibilityHidden
	case "public", "published", "scheduled", "open", "submissions_closed", "judging", "closed":
		return CompetitionVisibilityPublic
	default:
		return ""
	}
}

func normalizeCompetitionLifecycleOverride(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto", "automatic":
		return CompetitionLifecycleAuto
	case CompetitionLifecycleUpcoming, "scheduled":
		return CompetitionLifecycleUpcoming
	case CompetitionLifecycleOpen:
		return CompetitionLifecycleOpen
	case CompetitionLifecycleSubmissionsClosed, "closed_to_submissions", "public_gallery", "submissions_public", "public", "gallery":
		return CompetitionLifecycleSubmissionsClosed
	case CompetitionLifecycleClosed:
		return CompetitionLifecycleClosed
	default:
		return CompetitionLifecycleAuto
	}
}

func normalizeCompetitionJudgingMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CompetitionJudgingModeManual:
		return CompetitionJudgingModeManual
	case CompetitionJudgingModeAutomatic:
		return CompetitionJudgingModeAutomatic
	default:
		return CompetitionJudgingModeAutomatic
	}
}

func normalizeProjectInput(in ProjectInput) ProjectInput {
	in.CompetitionID = strings.TrimSpace(in.CompetitionID)
	in.CreatedByPersonID = strings.TrimSpace(in.CreatedByPersonID)
	in.Slug = normalizeSlug(in.Slug)
	in.Title = strings.TrimSpace(in.Title)
	in.ShortDescription = strings.TrimSpace(in.ShortDescription)
	in.Description = strings.TrimSpace(in.Description)
	in.DescriptionFormat = normalizeCompetitionDescriptionFormat(in.DescriptionFormat)
	in.ImageURL = strings.TrimSpace(in.ImageURL)
	in.ImageURLs = normalizeURLList(in.ImageURLs)
	if in.ImageURL != "" {
		in.ImageURLs = normalizeURLList(append([]string{in.ImageURL}, in.ImageURLs...))
	}
	if in.ImageURL == "" && len(in.ImageURLs) > 0 {
		in.ImageURL = in.ImageURLs[0]
	}
	in.GitHubURL = strings.TrimSpace(in.GitHubURL)
	in.DemoURL = strings.TrimSpace(in.DemoURL)
	in.VideoURL = strings.TrimSpace(in.VideoURL)
	in.SlidesURL = strings.TrimSpace(in.SlidesURL)
	in.DocsURL = strings.TrimSpace(in.DocsURL)
	tags := make([]string, 0, len(in.Tags))
	for _, tag := range in.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	in.Tags = tags
	return in
}

func normalizeURLList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func normalizeProjectStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProjectStatusSubmitted:
		return ProjectStatusSubmitted
	case ProjectStatusHidden, "withdrawn", "disqualified":
		return ProjectStatusHidden
	case ProjectStatusAdvanced, "finalist":
		return ProjectStatusAdvanced
	default:
		return ProjectStatusCreated
	}
}

func normalizeJudgeEventInput(in JudgeEventInput) JudgeEventInput {
	in.CompetitionID = strings.TrimSpace(in.CompetitionID)
	in.Name = strings.TrimSpace(in.Name)
	in.PlaybookType = normalizeJudgeEventType(in.PlaybookType)
	if in.RankLimit <= 0 {
		in.RankLimit = 4
	}
	return in
}

func normalizeScorecardInput(in ScorecardInput) ScorecardInput {
	in.JudgeEventID = strings.TrimSpace(in.JudgeEventID)
	in.ProjectID = strings.TrimSpace(in.ProjectID)
	in.JudgePersonID = strings.TrimSpace(in.JudgePersonID)
	in.Comments = strings.TrimSpace(in.Comments)
	return in
}

func normalizeScorecardRankingsInput(in ScorecardRankingsInput) ScorecardRankingsInput {
	in.JudgeEventID = strings.TrimSpace(in.JudgeEventID)
	in.JudgePersonID = strings.TrimSpace(in.JudgePersonID)
	rankings := make([]ScorecardRankingInput, 0, len(in.Rankings))
	for _, ranking := range in.Rankings {
		ranking.ProjectID = strings.TrimSpace(ranking.ProjectID)
		if ranking.ProjectID != "" && ranking.Rank > 0 {
			rankings = append(rankings, ranking)
		}
	}
	in.Rankings = rankings
	return in
}

func normalizeAwardInput(in AwardInput) AwardInput {
	in.CompetitionID = strings.TrimSpace(in.CompetitionID)
	in.SponsoredByOrgID = strings.TrimSpace(in.SponsoredByOrgID)
	in.AwardType = normalizeAwardType(in.AwardType)
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.JudgingInstructions = strings.TrimSpace(in.JudgingInstructions)
	if in.SponsoredByOrgID == "" {
		in.JudgingInstructions = ""
	}
	if in.MaxAwardees == nil {
		n := 1
		in.MaxAwardees = &n
	}
	in.Status = normalizeAwardStatus(in.Status)
	return in
}

func normalizePrizeInput(in PrizeInput) PrizeInput {
	in.AwardID = strings.TrimSpace(in.AwardID)
	in.PrizeType = normalizePrizeType(in.PrizeType)
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.ValueText = strings.TrimSpace(in.ValueText)
	in.PoolURL = strings.TrimSpace(in.PoolURL)
	in.Status = normalizePrizeStatus(in.Status)
	in.Comments = strings.TrimSpace(in.Comments)
	return in
}

func validatePrizeInput(in PrizeInput) (PrizeInput, error) {
	in = normalizePrizeInput(in)
	if in.AwardID == "" {
		return PrizeInput{}, fmt.Errorf("prize award id is required")
	}
	if in.Title == "" {
		return PrizeInput{}, fmt.Errorf("prize title is required")
	}
	if in.PrizeType == "" {
		return PrizeInput{}, fmt.Errorf("prize type is required")
	}
	value, err := strconv.ParseInt(in.ValueText, 10, 64)
	if err != nil || value <= 0 {
		return PrizeInput{}, fmt.Errorf("prize value must be a positive whole number of satoshis")
	}
	in.ValueText = strconv.FormatInt(value, 10)
	return in, nil
}

func normalizeSlug(slug string) string {
	slug = strings.TrimSpace(strings.ToLower(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug
}

func normalizeJudgeEventType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case JudgeTypeExpo:
		return JudgeTypeExpo
	case JudgeTypeFinals:
		return JudgeTypeFinals
	default:
		return ""
	}
}

func normalizeJudgeEventState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case JudgeEventStateOpen:
		return JudgeEventStateOpen
	case JudgeEventStateClosed, "review", "finalized", "skipped":
		return JudgeEventStateClosed
	default:
		return JudgeEventStatePending
	}
}

func normalizeJudgeType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case JudgeTypeExpo:
		return JudgeTypeExpo
	case JudgeTypeFinals:
		return JudgeTypeFinals
	default:
		return ""
	}
}

func normalizeAwardStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AwardStatusAvailable:
		return AwardStatusAvailable
	case AwardStatusUnawarded:
		return AwardStatusUnawarded
	case AwardStatusAwarded:
		return AwardStatusAwarded
	default:
		return AwardStatusDraft
	}
}

func normalizeAwardType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AwardTypeChallenge, "sponsor", "bounty":
		return AwardTypeChallenge
	case AwardTypeNormal, "ranked":
		return AwardTypeNormal
	default:
		return AwardTypeNormal
	}
}

func normalizePrizeType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PrizeTypeSats:
		return PrizeTypeSats
	case PrizeTypeInKind:
		return PrizeTypeInKind
	case PrizeTypeTickets:
		return PrizeTypeTickets
	case PrizeTypePooled:
		return PrizeTypePooled
	case PrizeTypeTrophy:
		return PrizeTypeTrophy
	default:
		return ""
	}
}

func normalizePrizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PrizeStatusNeedsFunds:
		return PrizeStatusNeedsFunds
	case PrizeStatusAwarded:
		return PrizeStatusAwarded
	case PrizeStatusPaid:
		return PrizeStatusPaid
	default:
		return PrizeStatusAvailable
	}
}

func normalizeProjectMemberRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return ProjectMemberRoleMember
	}
	return role
}

func newInviteToken() (string, string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", fmt.Errorf("generate project invite token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	return token, hashInviteToken(token), nil
}

func hashInviteToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
