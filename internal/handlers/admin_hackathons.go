package handlers

import (
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/payoutdocs"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

type HackathonAdminPage struct {
	Competitions         []*types.HackathonCompetition
	Conf                 *types.Conf
	Confs                []*types.Conf
	Competition          *types.HackathonCompetition
	Projects             []*types.HackathonProject
	NonFinalistProjects  []*types.HackathonProject
	ProjectTeams         map[string][]*types.ProjectMember
	ActiveTab            string
	JudgeEvents          []*types.JudgeEvent
	Judges               []*types.CompetitionJudge
	JudgeInviteLink      string
	JudgeInviteQRCodeURI string
	PeopleByID           map[string]*types.Speaker
	Scorecards           []*types.Scorecard
	ScoreSummaries       []*HackathonScoreSummary
	ScoreMode            string
	ScoreJudgeEventID    string
	Awards               []*types.Award
	ArchivedAwards       []*types.Award
	PrizesByAward        map[string][]*types.Prize
	AwardeesByAward      map[string][]*types.ProjectAward
	AwardOptInsByProject map[string][]*types.ProjectAwardOptIn
	PayoutAssignments    []*HackathonPayoutAssignment
	AwardDistributions   []*types.AwardDistribution
	ScheduleSegments     []*types.CompetitionScheduleSegment
	ScheduleEventsByID   map[string]HackathonScheduleEvent
	ProjectCount         int
	ScheduleSegmentCount int
	JudgeEventCount      int
	ScoreProjectCount    int
	AwardCount           int
	IsNew                bool
	SetupFromSchedule    bool
	SetupStep            int
	SeedScheduleBlocks   bool
	FlashMessage         string
	FlashError           string
	SearchQuery          string
	Sort                 string
	Year                 uint
}

type HackathonPayoutAssignment struct {
	Award                  *types.Award
	Project                *types.HackathonProject
	Recipients             []*types.HackathonPayoutRecipient
	Prizes                 []*HackathonPayoutPrize
	HasRemainingAllocation bool
}

type HackathonPayoutPrize struct {
	Prize                *types.Prize
	ConfiguredSats       int64
	AllocatedSats        int64
	RemainingSats        int64
	DistributionCount    int
	DistributedPersonIDs string
}

const (
	hackathonScoreModeAll    = "all"
	hackathonScoreModeExpo   = getters.JudgeTypeExpo
	hackathonScoreModeFinals = getters.JudgeTypeFinals
)

type HackathonScoreSummary struct {
	ProjectID        string
	ProjectTitle     string
	ProjectNumber    string
	Scorecards       int
	ScoredScorecards int
	Points           int
	PointsLabel      string
	RankAverage      string

	sortTitle      string
	rankAverage    float64
	hasRankAverage bool
}

type HackathonJudgeBallot struct {
	JudgePersonID string
	JudgeName     string
	Ranks         []HackathonJudgeBallotRank
	SubmittedAt   *time.Time
	HasRankings   bool
}

type HackathonJudgeBallotRank struct {
	Rank         int
	RankLabel    string
	ProjectID    string
	ProjectTitle string
}

type scoreSummaryAccumulator struct {
	summary   *HackathonScoreSummary
	events    map[string]*types.JudgeEvent
	rankSum   int
	rankCount int
}

func (p *HackathonAdminPage) ConfLabel(confID string) string {
	confID = strings.TrimSpace(confID)
	if confID == "" {
		return "Unknown conference"
	}
	for _, conf := range p.Confs {
		if conf == nil || conf.Ref != confID {
			continue
		}
		if conf.Tag != "" {
			return conf.Tag
		}
		if conf.Desc != "" {
			return conf.Desc
		}
	}
	return confID
}

func activeHackathonConfs(confs []*types.Conf) []*types.Conf {
	active := make([]*types.Conf, 0, len(confs))
	for _, conf := range confs {
		if conf != nil && conf.Active {
			active = append(active, conf)
		}
	}
	return active
}

func availableHackathonConfs(ctx *config.AppContext, confs []*types.Conf, currentCompetitionID string) []*types.Conf {
	active := activeHackathonConfs(confs)
	competitions, err := getters.ListCompetitions(ctx)
	if err != nil {
		ctx.Err.Printf("available hackathon confs: %s", err)
		return active
	}
	competitionByConf := make(map[string]string, len(competitions))
	for _, competition := range competitions {
		if competition != nil {
			competitionByConf[competition.ConferenceID] = competition.ID
		}
	}
	filtered := make([]*types.Conf, 0, len(active))
	for _, conf := range active {
		if conf == nil {
			continue
		}
		competitionID := competitionByConf[conf.Ref]
		if competitionID == "" || competitionID == currentCompetitionID {
			filtered = append(filtered, conf)
		}
	}
	return filtered
}

func hackathonSetupConf(confs []*types.Conf, raw string) *types.Conf {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, conf := range confs {
		if conf == nil {
			continue
		}
		if conf.Ref == raw || conf.Tag == raw {
			return conf
		}
	}
	return nil
}

func (p *HackathonAdminPage) SetupStep1Complete() bool {
	if p == nil || p.Competition == nil {
		return false
	}
	return strings.TrimSpace(p.Competition.ID) != "" &&
		strings.TrimSpace(p.Competition.Title) != "" &&
		strings.TrimSpace(p.Competition.Slug) != "" &&
		strings.TrimSpace(p.Competition.ConferenceID) != "" &&
		strings.TrimSpace(p.Competition.Visibility) != ""
}

func (p *HackathonAdminPage) SetupStep1URL() string {
	if p == nil || p.Competition == nil || strings.TrimSpace(p.Competition.ID) == "" {
		if p != nil && p.IsNew {
			return "/admin/hackathons/new"
		}
		return ""
	}
	return p.adminBaseURL(p.Competition) + "?setup=1"
}

func (p *HackathonAdminPage) SetupStep2URL() string {
	if !p.SetupStep1Complete() {
		return ""
	}
	return p.adminBaseURL(p.Competition) + "?setup=2"
}

func (p *HackathonAdminPage) SetupStep3URL() string {
	if !p.SetupStep1Complete() {
		return ""
	}
	return p.adminBaseURL(p.Competition) + "/awards?setup=3"
}

func (p *HackathonAdminPage) adminBaseURL(competition *types.HackathonCompetition) string {
	if conf := p.confForCompetition(competition); conf != nil && strings.TrimSpace(conf.Tag) != "" {
		return "/" + url.PathEscape(conf.Tag) + "/admin/hackathon"
	}
	if competition != nil && strings.TrimSpace(competition.ID) != "" {
		return "/admin/hackathons/" + url.PathEscape(competition.ID)
	}
	return "/admin/hackathons"
}

func hackathonAdminRequestURL(r *http.Request, competitionID, suffix string) string {
	if r != nil {
		if confTag := strings.TrimSpace(mux.Vars(r)["conf"]); confTag != "" {
			return "/" + url.PathEscape(confTag) + "/admin/hackathon" + suffix
		}
	}
	return "/admin/hackathons/" + url.PathEscape(competitionID) + suffix
}

func (p *HackathonAdminPage) EditURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition)
}

func (p *HackathonAdminPage) SetupURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "?setup=1"
}

func (p *HackathonAdminPage) ProjectsURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "/projects"
}

func (p *HackathonAdminPage) TimelineURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "/timeline"
}

func (p *HackathonAdminPage) JudgingURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "/judging"
}

func (p *HackathonAdminPage) ScoreReviewURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "/judging/scores"
}

func (p *HackathonAdminPage) AwardsURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "/awards"
}

func (p *HackathonAdminPage) PayoutsURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "/payouts"
}

func (p *HackathonAdminPage) ResultsURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "/results"
}

func (p *HackathonAdminPage) ResultsFinalized() bool {
	return p != nil && p.Competition != nil && p.Competition.ResultsFinalizedAt != nil
}

func (p *HackathonAdminPage) ResultsFinalizedLabel() string {
	if !p.ResultsFinalized() {
		return ""
	}
	label := p.Competition.ResultsFinalizedAt.Format("Jan 2, 2006 3:04 PM MST")
	if name := strings.TrimSpace(p.Competition.ResultsFinalizedName); name != "" {
		label += " by " + name
	}
	return label
}

func (p *HackathonAdminPage) HackathonURL(competition *types.HackathonCompetition) string {
	if p == nil {
		return ""
	}
	if p.Conf != nil && (competition == nil || p.Conf.Ref == competition.ConferenceID) {
		return hackathonURLForConf(p.Conf)
	}
	return hackathonURLForConf(p.confForCompetition(competition))
}

func (p *HackathonAdminPage) BackURL() string {
	if p != nil && p.Conf != nil && strings.TrimSpace(p.Conf.Tag) != "" {
		return "/" + url.PathEscape(p.Conf.Tag) + "/admin"
	}
	return "/admin/hackathons"
}

func (p *HackathonAdminPage) BackLabel() string {
	if p != nil && p.Conf != nil && strings.TrimSpace(p.Conf.Tag) != "" {
		return "Event admin"
	}
	return "Hackathons"
}

func (p *HackathonAdminPage) ConfURL(confID string) string {
	confID = strings.TrimSpace(confID)
	if confID == "" {
		return ""
	}
	for _, conf := range p.Confs {
		if conf == nil || conf.Ref != confID || strings.TrimSpace(conf.Tag) == "" {
			continue
		}
		return "/" + url.PathEscape(conf.Tag)
	}
	return ""
}

func (p *HackathonAdminPage) confForCompetition(competition *types.HackathonCompetition) *types.Conf {
	if p == nil || competition == nil {
		return nil
	}
	for _, conf := range p.Confs {
		if conf != nil && conf.Ref == competition.ConferenceID {
			return conf
		}
	}
	if p.Conf != nil && p.Conf.Ref == competition.ConferenceID {
		return p.Conf
	}
	return nil
}

func (p *HackathonAdminPage) ConferenceSchedulerURL(competition *types.HackathonCompetition) string {
	if p == nil || competition == nil {
		return ""
	}
	confURL := p.ConfURL(competition.ConferenceID)
	if confURL == "" {
		return ""
	}
	return confURL + "/admin/schedule"
}

func (p *HackathonAdminPage) VisibilityURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(competition) + "/visibility"
}

func (p *HackathonAdminPage) TimelineLabel(competition *types.HackathonCompetition) string {
	label, _ := hackathonNextMilestone(competition)
	return label
}

func (p *HackathonAdminPage) TimelineValue(competition *types.HackathonCompetition) string {
	_, value := hackathonNextMilestone(competition)
	return value
}

func (p *HackathonAdminPage) TimelineIsScheduleLink(competition *types.HackathonCompetition) bool {
	return hackathonMilestoneIsScheduleLink(competition)
}

func (p *HackathonAdminPage) ScheduleURL(competition *types.HackathonCompetition) string {
	return hackathonScheduleURLForConf(p.confForCompetition(competition))
}

func (p *HackathonAdminPage) ProjectPublicURL(project *types.HackathonProject) string {
	if p == nil || p.Competition == nil || project == nil {
		return ""
	}
	return hackathonURLForConf(p.confForCompetition(p.Competition)) + "/projects/" + url.PathEscape(project.ID)
}

func (p *HackathonAdminPage) ProjectAdminURL(project *types.HackathonProject) string {
	if p == nil || p.Competition == nil || project == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(p.Competition) + "/projects/" + url.PathEscape(project.ID)
}

func (p *HackathonAdminPage) AssignProjectNumbersURL() string {
	if p == nil || p.Competition == nil {
		return "/admin/hackathons"
	}
	return p.adminBaseURL(p.Competition) + "/projects/assign-numbers"
}

func (p *HackathonAdminPage) ProjectMembers(project *types.HackathonProject) []*types.ProjectMember {
	if p == nil || p.ProjectTeams == nil || project == nil {
		return nil
	}
	return p.ProjectTeams[project.ID]
}

func (p *HackathonAdminPage) ProjectNumberLabel(project *types.HackathonProject) string {
	if project == nil || project.ProjectNumber == nil {
		return "TBA"
	}
	return strconv.Itoa(*project.ProjectNumber)
}

func (p *HackathonAdminPage) ProjectStatusLabel(status string) string {
	return hackathonStatusLabel(status)
}

func (p *HackathonAdminPage) LifecycleOverrideLabel(value string) string {
	if label := hackathonLifecycleOverrideLabel(value); label != "" {
		return label
	}
	return "Automatic"
}

func (p *HackathonAdminPage) JudgeTypeLabel(judgeType string) string {
	switch strings.TrimSpace(judgeType) {
	case getters.JudgeTypeExpo:
		return "Expo"
	case getters.JudgeTypeFinals:
		return "Finals"
	case getters.JudgeTypeCoordinator:
		return "Coordinator"
	default:
		return strings.TrimSpace(judgeType)
	}
}

func (p *HackathonAdminPage) JudgeHasType(judge *types.CompetitionJudge, judgeType string) bool {
	if judge == nil {
		return false
	}
	for _, assignedType := range judge.JudgeTypes {
		if assignedType == judgeType {
			return true
		}
	}
	return len(judge.JudgeTypes) == 0 && judge.JudgeType == judgeType
}

func (p *HackathonAdminPage) JudgeRoleTypes(judge *types.CompetitionJudge) []string {
	if judge == nil {
		return nil
	}
	if len(judge.JudgeTypes) > 0 {
		return judge.JudgeTypes
	}
	if judge.JudgeType != "" {
		return []string{judge.JudgeType}
	}
	return nil
}

func (p *HackathonAdminPage) JudgeEventName(eventID string) string {
	if event := p.JudgeEvent(eventID); event != nil {
		return event.Name
	}
	return eventID
}

func (p *HackathonAdminPage) JudgeEvent(eventID string) *types.JudgeEvent {
	for _, event := range p.JudgeEvents {
		if event != nil && event.ID == eventID {
			return event
		}
	}
	return nil
}

func (p *HackathonAdminPage) JudgeEventTimeRange(event *types.JudgeEvent) string {
	return formatJudgeEventTimeRange(event, p.Conf)
}

func (p *HackathonAdminPage) JudgeEventTimeOnlyRange(event *types.JudgeEvent) string {
	return formatJudgeEventTimeOnlyRange(event, p.Conf)
}

func (p *HackathonAdminPage) RankLimit(event *types.JudgeEvent) int {
	return judgeEventRankLimit(event)
}

func (p *HackathonAdminPage) JudgingModeLabel() string {
	return judgingModeLabel(p.Competition)
}

func (p *HackathonAdminPage) JudgingMode() string {
	return competitionJudgingMode(p.Competition)
}

func (p *HackathonAdminPage) JudgingModeIsManual() bool {
	return p.JudgingMode() == getters.CompetitionJudgingModeManual
}

func (p *HackathonAdminPage) JudgingModeIsAutomatic() bool {
	return p.JudgingMode() == getters.CompetitionJudgingModeAutomatic
}

func (p *HackathonAdminPage) JudgeEventStateLabel(event *types.JudgeEvent) string {
	return judgeEventStateLabel(event)
}

func (p *HackathonAdminPage) JudgeEventEffectiveStateLabel(event *types.JudgeEvent) string {
	return judgeEventEffectiveStateLabel(p.Competition, event, time.Now())
}

func (p *HackathonAdminPage) JudgeEventAcceptsScores(event *types.JudgeEvent) bool {
	return judgeEventAcceptsScores(p.Competition, event, time.Now())
}

func (p *HackathonAdminPage) ScheduleSegmentTimeRange(segment *types.CompetitionScheduleSegment) string {
	if p == nil || segment == nil {
		return "Not scheduled"
	}
	for _, event := range p.JudgeEvents {
		if event != nil && event.ScheduleSegmentID == segment.ID {
			if event.StartsAt == nil && event.EndsAt == nil {
				return "Not scheduled"
			}
			return p.JudgeEventTimeRange(event)
		}
	}
	if p.ScheduleEventsByID == nil {
		return "Not scheduled"
	}
	event, ok := p.ScheduleEventsByID[segment.ID]
	if !ok || event.Time == nil {
		return "Not scheduled"
	}
	loc := time.Local
	if p.Conf != nil {
		loc = p.Conf.Loc()
	}
	start := event.Time.In(loc)
	dayLabel := "Schedule"
	if p.Conf != nil {
		dayLabel = fmt.Sprintf("Day %d", dayIndexFor(p.Conf, start))
	}
	if event.End == nil {
		return dayLabel + " · " + start.Format("3:04 PM MST")
	}
	end := event.End.In(loc)
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return dayLabel + " · " + start.Format("3:04 PM") + " - " + end.Format("3:04 PM MST")
	}
	endDayLabel := dayLabel
	if p.Conf != nil {
		endDayLabel = fmt.Sprintf("Day %d", dayIndexFor(p.Conf, end))
	}
	return dayLabel + " · " + start.Format("3:04 PM MST") + " - " + endDayLabel + " · " + end.Format("3:04 PM MST")
}

func (p *HackathonAdminPage) ScheduleSegmentVenue(segment *types.CompetitionScheduleSegment) string {
	if p == nil || segment == nil || p.ScheduleEventsByID == nil {
		return ""
	}
	event, ok := p.ScheduleEventsByID[segment.ID]
	if !ok {
		return ""
	}
	return event.Venue
}

func (p *HackathonAdminPage) ProjectTitle(projectID string) string {
	for _, project := range p.Projects {
		if project != nil && project.ID == projectID {
			return project.Title
		}
	}
	return projectID
}

func (p *HackathonAdminPage) JudgeName(personID string) string {
	for _, judge := range p.Judges {
		if judge == nil || judge.PersonID != personID {
			continue
		}
		if judge.Name != "" {
			return judge.Name
		}
		if judge.Email != "" {
			return judge.Email
		}
	}
	if p.PeopleByID != nil {
		if person := p.PeopleByID[personID]; person != nil {
			if person.Name != "" {
				return person.Name
			}
			if person.Email != "" {
				return person.Email
			}
		}
	}
	return personID
}

func (p *HackathonAdminPage) ScoreValue(value *int) string {
	if value == nil {
		return "-"
	}
	return strconv.Itoa(*value)
}

func (p *HackathonAdminPage) ScoreModeLabel(mode string) string {
	switch normalizeHackathonScoreMode(mode) {
	case hackathonScoreModeExpo:
		return "Expo"
	case hackathonScoreModeFinals:
		return "Finals"
	default:
		return "All scorecards"
	}
}

func (p *HackathonAdminPage) ScoreJudgeEventIs(event *types.JudgeEvent) bool {
	return p != nil && event != nil && p.ScoreJudgeEventID == event.ID
}

func (p *HackathonAdminPage) ScoresURLForJudgeEvent(event *types.JudgeEvent) string {
	if p == nil || p.Competition == nil {
		return "#"
	}
	if event == nil {
		return p.adminBaseURL(p.Competition) + "/judging/scores"
	}
	return p.adminBaseURL(p.Competition) + "/judging/scores?judge_event=" + url.QueryEscape(event.ID)
}

func (p *HackathonAdminPage) ScoreJudgeEventLabel() string {
	if p == nil {
		return "selected event"
	}
	if event := p.JudgeEvent(p.ScoreJudgeEventID); event != nil {
		return event.Name
	}
	return "selected event"
}

func (p *HackathonAdminPage) ScoreNextJudgeEvent() *types.JudgeEvent {
	if p == nil {
		return nil
	}
	return nextJudgeEvent(p.JudgeEvents, p.ScoreJudgeEventID)
}

func (p *HackathonAdminPage) ScoreHasNextJudgeEvent() bool {
	return p.ScoreNextJudgeEvent() != nil
}

func (p *HackathonAdminPage) ScoreNextJudgeEventLabel() string {
	if event := p.ScoreNextJudgeEvent(); event != nil {
		return judgeEventDisplayName(event)
	}
	return "next round"
}

func (p *HackathonAdminPage) ScoreBallotRanks() []HackathonJudgeBallotRank {
	if p == nil {
		return nil
	}
	limit := judgeEventRankLimit(p.JudgeEvent(p.ScoreJudgeEventID))
	return emptyJudgeBallotRanks(limit)
}

func (p *HackathonAdminPage) ScoreBallots() []*HackathonJudgeBallot {
	if p == nil {
		return nil
	}
	event := p.JudgeEvent(p.ScoreJudgeEventID)
	limit := judgeEventRankLimit(event)
	ballotByJudge := make(map[string]*HackathonJudgeBallot, len(p.Judges))
	ballots := make([]*HackathonJudgeBallot, 0, len(p.Judges))
	for _, judge := range p.Judges {
		if judge == nil || strings.TrimSpace(judge.PersonID) == "" {
			continue
		}
		ballot := &HackathonJudgeBallot{
			JudgePersonID: judge.PersonID,
			JudgeName:     p.JudgeName(judge.PersonID),
			Ranks:         emptyJudgeBallotRanks(limit),
		}
		ballotByJudge[judge.PersonID] = ballot
		ballots = append(ballots, ballot)
	}
	for _, scorecard := range p.Scorecards {
		if scorecard == nil || strings.TrimSpace(scorecard.JudgePersonID) == "" {
			continue
		}
		ballot := ballotByJudge[scorecard.JudgePersonID]
		if ballot == nil {
			ballot = &HackathonJudgeBallot{
				JudgePersonID: scorecard.JudgePersonID,
				JudgeName:     p.JudgeName(scorecard.JudgePersonID),
				Ranks:         emptyJudgeBallotRanks(limit),
			}
			ballotByJudge[scorecard.JudgePersonID] = ballot
			ballots = append(ballots, ballot)
		}
		if scorecard.SubmittedAt != nil && (ballot.SubmittedAt == nil || scorecard.SubmittedAt.After(*ballot.SubmittedAt)) {
			submittedAt := *scorecard.SubmittedAt
			ballot.SubmittedAt = &submittedAt
		}
		if scorecard.Rank == nil || *scorecard.Rank < 1 || *scorecard.Rank > len(ballot.Ranks) {
			continue
		}
		ballot.HasRankings = true
		rank := &ballot.Ranks[*scorecard.Rank-1]
		rank.ProjectID = scorecard.ProjectID
		rank.ProjectTitle = p.ProjectTitle(scorecard.ProjectID)
	}
	sort.SliceStable(ballots, func(i, j int) bool {
		return strings.ToLower(ballots[i].JudgeName) < strings.ToLower(ballots[j].JudgeName)
	})
	return ballots
}

func (p *HackathonAdminPage) ScorecardPoints(scorecard *types.Scorecard) string {
	if scorecard == nil {
		return "-"
	}
	if scorecard.Rank == nil {
		return "-"
	}
	event := p.JudgeEvent(scorecard.JudgeEventID)
	points := rankPoints(event, *scorecard.Rank)
	if points <= 0 {
		return "-"
	}
	return strconv.Itoa(points)
}

func (p *HackathonAdminPage) AwardStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case getters.AwardStatusDraft:
		return "Draft"
	case getters.AwardStatusAvailable:
		return "Available"
	case getters.AwardStatusUnawarded:
		return "Unawarded"
	case getters.AwardStatusAwarded:
		return "Awarded"
	default:
		return strings.TrimSpace(status)
	}
}

func (p *HackathonAdminPage) PrizeTypeLabel(prizeType string) string {
	switch strings.TrimSpace(prizeType) {
	case getters.PrizeTypeSats:
		return "Sats"
	case getters.PrizeTypeInKind:
		return "In-kind"
	case getters.PrizeTypeTickets:
		return "Tickets"
	case getters.PrizeTypePooled:
		return "Pooled"
	case getters.PrizeTypeTrophy:
		return "Trophy"
	default:
		return strings.TrimSpace(prizeType)
	}
}

func (p *HackathonAdminPage) PrizeStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case getters.PrizeStatusAvailable:
		return "Available"
	case getters.PrizeStatusNeedsFunds:
		return "Needs funds"
	case getters.PrizeStatusAwarded:
		return "Awarded"
	case getters.PrizeStatusPaid:
		return "Paid"
	default:
		return strings.TrimSpace(status)
	}
}

func (p *HackathonAdminPage) PrizeValueInput(prize *types.Prize) string {
	sats := prizeValueSats(prize)
	if sats <= 0 {
		return ""
	}
	return strconv.FormatInt(sats, 10)
}

func (p *HackathonAdminPage) PrizeValueLabel(prize *types.Prize) string {
	return prizeValueLabel(prize)
}

func (p *HackathonAdminPage) AwardPrizes(award *types.Award) []*types.Prize {
	if p == nil || p.PrizesByAward == nil || award == nil {
		return nil
	}
	return p.PrizesByAward[award.ID]
}

func (p *HackathonAdminPage) AwardArchivedAtLabel(award *types.Award) string {
	if award == nil || award.ArchivedAt == nil {
		return ""
	}
	return award.ArchivedAt.Format("Jan 2, 2006 3:04 PM")
}

func (p *HackathonAdminPage) Awardees(award *types.Award) []*types.ProjectAward {
	if p == nil || p.AwardeesByAward == nil || award == nil {
		return nil
	}
	return p.AwardeesByAward[award.ID]
}

func (p *HackathonAdminPage) AwardCanAssign(award *types.Award) bool {
	if award == nil || p.ResultsFinalized() {
		return false
	}
	return award.MaxAwardees == nil || len(p.Awardees(award)) < *award.MaxAwardees
}

func (p *HackathonAdminPage) ProjectHasAward(projectID string, award *types.Award) bool {
	if award == nil {
		return false
	}
	for _, assignment := range p.Awardees(award) {
		if assignment != nil && assignment.ProjectID == projectID {
			return true
		}
	}
	return false
}

func (p *HackathonAdminPage) ProjectAssignedAwards(projectID string) []*types.Award {
	if p == nil {
		return nil
	}
	assigned := make([]*types.Award, 0)
	for _, award := range p.Awards {
		if p.ProjectHasAward(projectID, award) {
			assigned = append(assigned, award)
		}
	}
	return assigned
}

func (p *HackathonAdminPage) ProjectAssignableAwards(projectID string) []*types.Award {
	if p == nil {
		return nil
	}
	assignable := make([]*types.Award, 0)
	for _, award := range p.Awards {
		if award != nil && (!award.FinalistsOnly || p.ProjectIsFinalist(projectID)) && !p.ProjectHasAward(projectID, award) && p.AwardCanAssign(award) {
			assignable = append(assignable, award)
		}
	}
	return assignable
}

func (p *HackathonAdminPage) ProjectIsFinalist(projectID string) bool {
	if p == nil {
		return false
	}
	for _, projects := range [][]*types.HackathonProject{p.Projects, p.NonFinalistProjects} {
		for _, project := range projects {
			if project != nil && project.ID == projectID {
				return project.Status == getters.ProjectStatusAdvanced
			}
		}
	}
	return false
}

func (p *HackathonAdminPage) AwardAssignmentLimitMessage(award *types.Award) string {
	if p.ResultsFinalized() {
		return "Results are finalized. Reopen results before changing award recipients."
	}
	if award == nil || award.MaxAwardees == nil || p.AwardCanAssign(award) {
		return ""
	}
	count := len(p.Awardees(award))
	return fmt.Sprintf("Awardee limit reached (%d of %d). Remove a winner before assigning another project.", count, *award.MaxAwardees)
}

func (p *HackathonAdminPage) ProjectSelectLabel(project *types.HackathonProject) string {
	if project == nil {
		return ""
	}
	if project.ProjectNumber != nil {
		return "#" + strconv.Itoa(*project.ProjectNumber) + " - " + project.Title
	}
	return project.Title
}

func (p *HackathonAdminPage) ProjectSelectLabelForAward(project *types.HackathonProject, award *types.Award) string {
	label := p.ProjectSelectLabel(project)
	if award != nil && award.OptInRequired && p.ProjectOptedIntoAward(project, award) {
		label += " (opted in)"
	}
	return label
}

func (p *HackathonAdminPage) ProjectAwardOptIns(project *types.HackathonProject) []*types.ProjectAwardOptIn {
	if p == nil || p.AwardOptInsByProject == nil || project == nil {
		return nil
	}
	return p.AwardOptInsByProject[project.ID]
}

func (p *HackathonAdminPage) ProjectOptedIntoAward(project *types.HackathonProject, award *types.Award) bool {
	if project == nil || award == nil {
		return false
	}
	for _, optIn := range p.ProjectAwardOptIns(project) {
		if optIn != nil && optIn.AwardID == award.ID {
			return true
		}
	}
	return false
}

func (p *HackathonAdminPage) ProjectAwardNumber(award *types.ProjectAward) string {
	if award == nil || award.ProjectNumber == nil {
		return "TBA"
	}
	return strconv.Itoa(*award.ProjectNumber)
}

func (p *HackathonAdminPage) OptionalIntLabel(value *int) string {
	if value == nil {
		return "Unlimited"
	}
	return strconv.Itoa(*value)
}

func (p *HackathonAdminPage) PercentLabel(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64) + "%"
}

func populateAdminHackathonCounts(ctx *config.AppContext, page *HackathonAdminPage) {
	if page == nil || page.Competition == nil {
		return
	}
	competitionID := page.Competition.ID
	projects := page.Projects
	if projects == nil {
		var err error
		projects, err = getters.ListProjectsForCompetition(ctx, competitionID, types.HackathonViewer{Admin: true})
		if err != nil {
			ctx.Err.Printf("/admin/hackathons/%s count projects: %s", competitionID, err)
		}
	}
	page.ProjectCount = len(projects)
	if page.ScoreSummaries != nil {
		page.ScoreProjectCount = len(page.ScoreSummaries)
	} else {
		page.ScoreProjectCount = len(projects)
	}

	segments := page.ScheduleSegments
	if segments == nil {
		var err error
		segments, err = getters.ListCompetitionScheduleSegments(ctx, competitionID)
		if err != nil {
			ctx.Err.Printf("/admin/hackathons/%s count schedule segments: %s", competitionID, err)
		}
	}
	page.ScheduleSegmentCount = len(segments)

	events := page.JudgeEvents
	if events == nil {
		var err error
		events, err = getters.ListJudgeEvents(ctx, competitionID)
		if err != nil {
			ctx.Err.Printf("/admin/hackathons/%s count judge events: %s", competitionID, err)
		}
	}
	events = timelineJudgeEvents(events)
	page.JudgeEventCount = len(events)

	awards := page.Awards
	if awards == nil {
		var err error
		awards, err = getters.ListAwardsForCompetition(ctx, competitionID)
		if err != nil {
			ctx.Err.Printf("/admin/hackathons/%s count awards: %s", competitionID, err)
		}
	}
	page.AwardCount = len(awards)
}

func HackathonAdminList(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	searchQuery, sortMode := hackathonListControls(r)
	competitions, err := getters.ListCompetitions(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons list competitions: %s", err)
		http.Error(w, "Unable to load hackathons", http.StatusInternalServerError)
		return
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons list confs: %s", err)
		http.Error(w, "Unable to load conferences", http.StatusInternalServerError)
		return
	}
	competitions = applyHackathonListControls(competitions, confs, searchQuery, sortMode)
	page := &HackathonAdminPage{
		Competitions: competitions,
		Confs:        confs,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		SearchQuery:  searchQuery,
		Sort:         sortMode,
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathons.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons template: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminProjects(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects conf: %s", competitionID, err)
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, types.HackathonViewer{Admin: true})
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects list: %s", competitionID, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return
	}
	teams := make(map[string][]*types.ProjectMember, len(projects))
	for _, project := range projects {
		if project == nil {
			continue
		}
		members, err := getters.ListProjectMembers(ctx, project.ID)
		if err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/projects/%s members: %s", competitionID, project.ID, err)
			http.Error(w, "Unable to load project members", http.StatusInternalServerError)
			return
		}
		teams[project.ID] = members
	}
	optIns, err := getters.ListProjectAwardOptInsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects award opt-ins: %s", competitionID, err)
		http.Error(w, "Unable to load project award opt-ins", http.StatusInternalServerError)
		return
	}
	page := &HackathonAdminPage{
		Competition:          competition,
		Conf:                 conf,
		Projects:             projects,
		ProjectTeams:         teams,
		ActiveTab:            "projects",
		AwardOptInsByProject: projectAwardOptInsByProject(optIns),
		FlashMessage:         r.URL.Query().Get("flash"),
		FlashError:           r.URL.Query().Get("error"),
		Year:                 helpers.CurrentYear(),
	}
	if r.URL.Query().Get("setup") == "3" {
		page.SetupStep = 3
		page.ActiveTab = ""
	}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_projects.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminCreateProject(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/projects")
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	in, status, projectNumber, err := adminProjectInputFromRequest(w, r, competition.ID)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	teamPersonIDs := personIDsFromForm(r, "TeamPersonID")
	if len(teamPersonIDs) > 0 {
		if err := validatePersonIDs(ctx, teamPersonIDs); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/projects selected people: %s", competitionID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		in.CreatedByPersonID = teamPersonIDs[0]
	}
	in.Slug, err = generatedProjectSlug()
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects create slug: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to create project ID"), http.StatusSeeOther)
		return
	}
	projectID, err := getters.CreateProject(ctx, in)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects create: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateProjectAdminFields(ctx, competition.ID, projectID, status, projectNumber); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects/%s create admin fields: %s", competitionID, projectID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project created, but admin fields could not be saved: "+err.Error()), http.StatusSeeOther)
		return
	}
	for _, personID := range teamPersonIDs[1:] {
		if err := getters.AddProjectMember(ctx, projectID, personID, getters.ProjectMemberRoleMember); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/projects/%s add member %s: %s", competitionID, projectID, personID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project created, but selected team members could not be added: "+err.Error()), http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project created"), http.StatusSeeOther)
}

func HackathonAdminUpdateProject(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	vars := mux.Vars(r)
	competitionID := vars["competitionID"]
	projectID := vars["projectID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/projects")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		if r.FormValue("AutoSave") == "1" {
			http.Error(w, "Bad form", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	autoSave := r.FormValue("AutoSave") == "1"
	projectNumber, err := optionalIntFromForm(r, "ProjectNumber", "project number", 1, 0)
	if err != nil {
		if autoSave {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateProjectAdminFields(ctx, competitionID, projectID, r.FormValue("Status"), projectNumber); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects/%s update: %s", competitionID, projectID, err)
		if autoSave {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if autoSave {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project updated"), http.StatusSeeOther)
}

func adminProjectInputFromRequest(w http.ResponseWriter, r *http.Request, competitionID string) (getters.ProjectInput, string, *int, error) {
	in, err := projectInputFromRequest(nil, w, r, competitionID)
	if err != nil {
		return getters.ProjectInput{}, "", nil, err
	}
	projectNumber, err := optionalIntFromForm(r, "ProjectNumber", "project number", 1, 0)
	if err != nil {
		return getters.ProjectInput{}, "", nil, err
	}
	status := strings.TrimSpace(r.FormValue("Status"))
	if status == "" {
		status = getters.ProjectStatusSubmitted
	}
	return in, status, projectNumber, nil
}

func personIDsFromForm(r *http.Request, field string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, value := range r.Form[field] {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func validatePersonIDs(ctx *config.AppContext, personIDs []string) error {
	for _, personID := range personIDs {
		person, err := getters.FetchSpeakerByID(ctx, personID)
		if err != nil {
			return fmt.Errorf("unable to load selected person")
		}
		if person == nil {
			return fmt.Errorf("selected person was not found")
		}
	}
	return nil
}

func timelineJudgeEvents(events []*types.JudgeEvent) []*types.JudgeEvent {
	if len(events) == 0 {
		return events
	}
	out := make([]*types.JudgeEvent, 0, len(events))
	for _, event := range events {
		if event != nil && strings.TrimSpace(event.ScheduleSegmentID) != "" {
			out = append(out, event)
		}
	}
	return out
}

func HackathonAdminAssignProjectNumbers(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/projects")
	count, err := getters.AssignMissingProjectNumbers(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects assign numbers: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	message := fmt.Sprintf("Assigned %d project number", count)
	if count != 1 {
		message += "s"
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(message), http.StatusSeeOther)
}

func HackathonAdminScoreReview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores conf: %s", competitionID, err)
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, types.HackathonViewer{Admin: true})
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores projects: %s", competitionID, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return
	}
	events, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores events: %s", competitionID, err)
		http.Error(w, "Unable to load judge events", http.StatusInternalServerError)
		return
	}
	events = timelineJudgeEvents(events)
	judges, err := getters.ListCompetitionJudges(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores judges: %s", competitionID, err)
		http.Error(w, "Unable to load judges", http.StatusInternalServerError)
		return
	}
	scorecards, err := getters.ListScorecardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores scorecards: %s", competitionID, err)
		http.Error(w, "Unable to load scorecards", http.StatusInternalServerError)
		return
	}
	scoreJudgeEventID := selectedScoreJudgeEventID(competition, events, r.URL.Query().Get("judge_event"))
	filteredScorecards := filterHackathonScorecardsByJudgeEvent(scorecards, scoreJudgeEventID)
	scoreProjects := projectsForJudgeEvent(projects, events, scoreJudgeEventID)
	nonFinalistProjects := make([]*types.HackathonProject, 0)
	if scoreJudgeEventID != "" && nextJudgeEvent(events, scoreJudgeEventID) == nil {
		for _, project := range projects {
			if project != nil && project.Status == getters.ProjectStatusSubmitted {
				nonFinalistProjects = append(nonFinalistProjects, project)
			}
		}
	}
	peopleByID, err := scorecardJudgePeopleByID(ctx, scorecards)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores people: %s", competitionID, err)
	}
	awards, err := getters.ListAwardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores awards: %s", competitionID, err)
		http.Error(w, "Unable to load awards", http.StatusInternalServerError)
		return
	}
	projectAwards, err := getters.ListProjectAwardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores award assignments: %s", competitionID, err)
		http.Error(w, "Unable to load award assignments", http.StatusInternalServerError)
		return
	}
	awardeesByAward := make(map[string][]*types.ProjectAward)
	for _, assignment := range projectAwards {
		if assignment != nil {
			awardeesByAward[assignment.AwardID] = append(awardeesByAward[assignment.AwardID], assignment)
		}
	}
	page := &HackathonAdminPage{
		Competition:         competition,
		Conf:                conf,
		Projects:            scoreProjects,
		NonFinalistProjects: nonFinalistProjects,
		JudgeEvents:         events,
		Judges:              judges,
		PeopleByID:          peopleByID,
		Scorecards:          filteredScorecards,
		ScoreSummaries:      hackathonScoreSummaries(scoreProjects, filteredScorecards, events),
		Awards:              awards,
		AwardeesByAward:     awardeesByAward,
		ActiveTab:           "scores",
		ScoreMode:           hackathonScoreModeAll,
		ScoreJudgeEventID:   scoreJudgeEventID,
		FlashMessage:        r.URL.Query().Get("flash"),
		FlashError:          r.URL.Query().Get("error"),
		Year:                helpers.CurrentYear(),
	}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_scores.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminTimeline(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/timeline conf: %s", competitionID, err)
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/timeline confs: %s", competitionID, err)
		http.Error(w, "Unable to load conferences", http.StatusInternalServerError)
		return
	}
	scheduleEvents, err := loadHackathonScheduleEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/timeline schedule events: %s", competitionID, err)
		http.Error(w, "Unable to load schedule events", http.StatusInternalServerError)
		return
	}
	scheduleEventsByID := scheduleEventsBySegmentID(scheduleEvents)
	judgeEvents, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/timeline judge events: %s", competitionID, err)
		http.Error(w, "Unable to load schedule event times", http.StatusInternalServerError)
		return
	}
	judgeEvents = timelineJudgeEvents(judgeEvents)
	scheduleSegments := loadCompetitionScheduleSegments(ctx, competition.ID)
	sortScheduleSegmentsByScheduleTime(scheduleSegments, scheduleEventsByID)
	page := &HackathonAdminPage{
		Competition:        competition,
		Conf:               conf,
		Confs:              confs,
		ActiveTab:          "timeline",
		JudgeEvents:        judgeEvents,
		ScheduleSegments:   scheduleSegments,
		ScheduleEventsByID: scheduleEventsByID,
		FlashMessage:       r.URL.Query().Get("flash"),
		FlashError:         r.URL.Query().Get("error"),
		Year:               helpers.CurrentYear(),
	}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_timeline.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/timeline template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminUpdateTimeline(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/timeline")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	segments, err := competitionScheduleSegmentInputsFromRequest(r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.ReplaceCompetitionScheduleSegments(ctx, competitionID, segments); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/timeline schedule segments: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Timeline could not be saved"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Timeline saved"), http.StatusSeeOther)
}

func HackathonAdminPersonSearch(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	writePersonSearchResults(w, r, ctx)
}

func HackathonAdminAdvanceProjects(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging/scores")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	topN, err := optionalIntFromForm(r, "TopN", "project count", 1, 0)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if topN == nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("project count is required"), http.StatusSeeOther)
		return
	}
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, types.HackathonViewer{Admin: true})
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/advance projects: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to load projects"), http.StatusSeeOther)
		return
	}
	events, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/advance events: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to load judge events"), http.StatusSeeOther)
		return
	}
	events = timelineJudgeEvents(events)
	eventID := selectedScoreJudgeEventID(competition, events, r.FormValue("JudgeEventID"))
	dest = dest + "?judge_event=" + url.QueryEscape(eventID)
	if eventID == "" {
		http.Redirect(w, r, dest+"&error="+url.QueryEscape("judging event is required"), http.StatusSeeOther)
		return
	}
	if nextJudgeEvent(events, eventID) == nil {
		http.Redirect(w, r, dest+"&error="+url.QueryEscape("This is the final judging round. Assign awards from the rankings instead."), http.StatusSeeOther)
		return
	}
	scorecards, err := getters.ListScorecardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/advance scorecards: %s", competitionID, err)
		http.Redirect(w, r, dest+"&error="+url.QueryEscape("Unable to load scorecards"), http.StatusSeeOther)
		return
	}
	event := judgeEventByID(events, eventID)
	filteredScorecards := filterHackathonScorecardsByJudgeEvent(scorecards, eventID)
	advanced := hackathonAdvancedSelectionsFromScorecards(projects, filteredScorecards, events, event, *topN)
	if len(advanced) == 0 {
		http.Redirect(w, r, dest+"&error="+url.QueryEscape("No scored eligible projects found for "+judgeEventDisplayName(event)+"."), http.StatusSeeOther)
		return
	}
	advancedIDs := make(map[string]bool, len(advanced))
	for _, project := range advanced {
		if project == nil {
			continue
		}
		advancedIDs[project.ID] = true
		if err := getters.UpdateProjectAdminFields(ctx, competition.ID, project.ID, getters.ProjectStatusAdvanced, project.ProjectNumber); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/judging/advance project %s: %s", competitionID, project.ID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
	}
	demoted := 0
	for _, project := range projects {
		if project == nil || project.Status != getters.ProjectStatusAdvanced || advancedIDs[project.ID] {
			continue
		}
		if !projectEligibleForJudgeEvent(project, events, event) {
			continue
		}
		if err := getters.UpdateProjectAdminFields(ctx, competition.ID, project.ID, getters.ProjectStatusSubmitted, project.ProjectNumber); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/judging/advance demote project %s: %s", competitionID, project.ID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		demoted++
	}
	message := fmt.Sprintf("Advanced %d project", len(advanced))
	if len(advanced) != 1 {
		message += "s"
	}
	if demoted > 0 {
		message += fmt.Sprintf("; moved %d back to submitted", demoted)
	}
	message += " using " + judgeEventDisplayName(event)
	http.Redirect(w, r, dest+"&flash="+url.QueryEscape(message), http.StatusSeeOther)
}

func nextJudgeEvent(events []*types.JudgeEvent, eventID string) *types.JudgeEvent {
	found := false
	for _, event := range events {
		if event == nil {
			continue
		}
		if found {
			return event
		}
		if event.ID == eventID {
			found = true
		}
	}
	return nil
}

func HackathonAdminRemoveJudgeBallot(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging/scores")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	eventID := strings.TrimSpace(r.FormValue("JudgeEventID"))
	judgePersonID := strings.TrimSpace(r.FormValue("JudgePersonID"))
	if eventID != "" {
		dest += "?judge_event=" + url.QueryEscape(eventID)
	}
	if eventID == "" || judgePersonID == "" {
		http.Redirect(w, r, appendAdminScoresMessage(dest, "error", "judge event and judge are required"), http.StatusSeeOther)
		return
	}
	if err := getters.DeleteScorecardRankings(ctx, competitionID, eventID, judgePersonID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores/remove-ballot event=%s judge=%s: %s", competitionID, eventID, judgePersonID, err)
		http.Redirect(w, r, appendAdminScoresMessage(dest, "error", err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendAdminScoresMessage(dest, "flash", "Judge ballot removed"), http.StatusSeeOther)
}

func HackathonAdminAwards(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards conf: %s", competitionID, err)
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	awards, err := getters.ListAwardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards list awards: %s", competitionID, err)
		http.Error(w, "Unable to load awards", http.StatusInternalServerError)
		return
	}
	archivedAwards, err := getters.ListArchivedAwardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards list archived awards: %s", competitionID, err)
		http.Error(w, "Unable to load archived awards", http.StatusInternalServerError)
		return
	}
	prizes, err := getters.ListPrizesForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards list prizes: %s", competitionID, err)
		http.Error(w, "Unable to load prizes", http.StatusInternalServerError)
		return
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, types.HackathonViewer{Admin: true})
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards list projects: %s", competitionID, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return
	}
	projectAwards, err := getters.ListProjectAwardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards list awardees: %s", competitionID, err)
		http.Error(w, "Unable to load awardees", http.StatusInternalServerError)
		return
	}
	optIns, err := getters.ListProjectAwardOptInsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards list opt-ins: %s", competitionID, err)
		http.Error(w, "Unable to load award opt-ins", http.StatusInternalServerError)
		return
	}
	prizesByAward := make(map[string][]*types.Prize)
	for _, prize := range prizes {
		if prize != nil {
			prizesByAward[prize.AwardID] = append(prizesByAward[prize.AwardID], prize)
		}
	}
	awardeesByAward := make(map[string][]*types.ProjectAward)
	for _, award := range projectAwards {
		if award != nil {
			awardeesByAward[award.AwardID] = append(awardeesByAward[award.AwardID], award)
		}
	}
	page := &HackathonAdminPage{
		Competition:          competition,
		Conf:                 conf,
		Projects:             projects,
		ActiveTab:            "awards",
		Awards:               awards,
		ArchivedAwards:       archivedAwards,
		PrizesByAward:        prizesByAward,
		AwardeesByAward:      awardeesByAward,
		AwardOptInsByProject: projectAwardOptInsByProject(optIns),
		FlashMessage:         r.URL.Query().Get("flash"),
		FlashError:           r.URL.Query().Get("error"),
		Year:                 helpers.CurrentYear(),
	}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_awards.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func projectAwardOptInsByProject(optIns []*types.ProjectAwardOptIn) map[string][]*types.ProjectAwardOptIn {
	byProject := make(map[string][]*types.ProjectAwardOptIn)
	for _, optIn := range optIns {
		if optIn != nil {
			byProject[optIn.ProjectID] = append(byProject[optIn.ProjectID], optIn)
		}
	}
	return byProject
}

func HackathonAdminPayouts(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil || competition == nil {
		handle404(w, r, ctx)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil || conf == nil {
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		http.Error(w, "Unable to load awards", http.StatusInternalServerError)
		return
	}
	prizes, err := getters.ListPrizesForCompetition(ctx, competitionID)
	if err != nil {
		http.Error(w, "Unable to load prizes", http.StatusInternalServerError)
		return
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competitionID, types.HackathonViewer{Admin: true})
	if err != nil {
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return
	}
	assignments, err := getters.ListProjectAwardsForCompetition(ctx, competitionID)
	if err != nil {
		http.Error(w, "Unable to load award assignments", http.StatusInternalServerError)
		return
	}
	distributions, err := getters.ListAwardDistributions(ctx, competitionID)
	if err != nil {
		http.Error(w, "Unable to load distributions", http.StatusInternalServerError)
		return
	}
	recipientsByProject, err := getters.ListCashPayoutRecipients(ctx, competitionID)
	if err != nil {
		http.Error(w, "Unable to load payout recipients", http.StatusInternalServerError)
		return
	}
	awardByID := map[string]*types.Award{}
	prizesByAward := map[string][]*types.Prize{}
	projectByID := map[string]*types.HackathonProject{}
	for _, award := range awards {
		if award != nil {
			awardByID[award.ID] = award
		}
	}
	for _, prize := range prizes {
		if prize != nil && prize.PrizeType == getters.PrizeTypeSats {
			prizesByAward[prize.AwardID] = append(prizesByAward[prize.AwardID], prize)
		}
	}
	for _, project := range projects {
		if project != nil {
			projectByID[project.ID] = project
		}
	}
	allocatedByPrize := map[string]int64{}
	distributionCountByPrize := map[string]int{}
	distributedPeopleByPrize := map[string][]string{}
	cashDistributions := make([]*types.AwardDistribution, 0, len(distributions))
	for _, distribution := range distributions {
		if distribution == nil || distribution.DistributionType != getters.PrizeTypeSats {
			continue
		}
		cashDistributions = append(cashDistributions, distribution)
		if distribution.Status == "cancelled" {
			continue
		}
		allocationKey := distribution.ProjectID + ":" + distribution.PrizeID
		distributionCountByPrize[allocationKey]++
		distributedPeopleByPrize[allocationKey] = append(distributedPeopleByPrize[allocationKey], distribution.PersonID)
		if distribution.AmountSats != nil {
			allocatedByPrize[allocationKey] += *distribution.AmountSats
		}
	}
	rows := make([]*HackathonPayoutAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment == nil || awardByID[assignment.AwardID] == nil || projectByID[assignment.ProjectID] == nil {
			continue
		}
		configuredPrizes := prizesByAward[assignment.AwardID]
		if len(configuredPrizes) == 0 {
			continue
		}
		cashPrizes := make([]*HackathonPayoutPrize, 0, len(configuredPrizes))
		hasRemainingAllocation := false
		for _, prize := range configuredPrizes {
			configured, parseErr := strconv.ParseInt(strings.TrimSpace(prize.ValueText), 10, 64)
			if parseErr != nil || configured <= 0 {
				continue
			}
			allocationKey := assignment.ProjectID + ":" + prize.ID
			allocated := allocatedByPrize[allocationKey]
			remaining := configured - allocated
			if remaining < 0 {
				remaining = 0
			}
			if remaining > 0 && distributionCountByPrize[allocationKey] < len(recipientsByProject[assignment.ProjectID]) {
				hasRemainingAllocation = true
			}
			cashPrizes = append(cashPrizes, &HackathonPayoutPrize{
				Prize: prize, ConfiguredSats: configured, AllocatedSats: allocated,
				RemainingSats: remaining, DistributionCount: distributionCountByPrize[allocationKey],
				DistributedPersonIDs: strings.Join(distributedPeopleByPrize[allocationKey], ","),
			})
		}
		rows = append(rows, &HackathonPayoutAssignment{Award: awardByID[assignment.AwardID], Project: projectByID[assignment.ProjectID], Recipients: recipientsByProject[assignment.ProjectID], Prizes: cashPrizes, HasRemainingAllocation: hasRemainingAllocation})
	}
	page := &HackathonAdminPage{Competition: competition, Conf: conf, ActiveTab: "payouts", PayoutAssignments: rows, AwardDistributions: cashDistributions, FlashMessage: r.URL.Query().Get("flash"), FlashError: r.URL.Query().Get("error"), Year: helpers.CurrentYear()}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_payouts.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/payouts template: %s", competitionID, err)
		http.Error(w, "Unable to load payout dashboard", http.StatusInternalServerError)
	}
}

func HackathonAdminCreateDistribution(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/payouts")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	var amount *int64
	if raw := strings.TrimSpace(r.FormValue("AmountSats")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Satoshi amount must be a positive whole number"), http.StatusSeeOther)
			return
		}
		amount = &value
	}
	if amount == nil {
		value, valueErr := getters.CashPrizeValueSats(ctx, competitionID, r.FormValue("AwardID"), r.FormValue("PrizeID"))
		if valueErr != nil {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape(valueErr.Error()), http.StatusSeeOther)
			return
		}
		amount = &value
	}
	_, err := getters.CreateAwardDistribution(ctx, getters.AwardDistributionInput{CompetitionID: competitionID, AwardID: r.FormValue("AwardID"), ProjectID: r.FormValue("ProjectID"), PrizeID: r.FormValue("PrizeID"), PersonID: r.FormValue("PersonID"), DistributionType: getters.PrizeTypeSats, AmountSats: amount, Notes: r.FormValue("Notes")})
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Award distribution added"), http.StatusSeeOther)
}

func HackathonAdminPrepareDistributions(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/payouts")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	created, err := getters.PrepareCashPrizeDistributions(ctx, competitionID,
		r.FormValue("AwardID"), r.FormValue("ProjectID"), r.FormValue("PrizeID"))
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	message := fmt.Sprintf("Prepared %d equal team payout distribution", created)
	if created != 1 {
		message += "s"
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(message), http.StatusSeeOther)
}

func HackathonAdminUpdateDistribution(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireHackathonAdmin(w, r, ctx)
	if id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/payouts")
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	completedBy := ""
	if id.Speaker != nil {
		completedBy = id.Speaker.ID
	}
	if err := getters.UpdateAwardDistribution(ctx, competitionID, mux.Vars(r)["distributionID"], r.FormValue("Status"), r.FormValue("Notes"), completedBy); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Distribution updated"), http.StatusSeeOther)
}

func HackathonAdminDownloadTaxForm(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	personID := mux.Vars(r)["personID"]
	recipientsByProject, err := getters.ListCashPayoutRecipients(ctx, competitionID)
	if err != nil {
		http.Error(w, "Unable to verify payout recipient", http.StatusInternalServerError)
		return
	}
	allowed := false
	for _, recipients := range recipientsByProject {
		for _, recipient := range recipients {
			if recipient != nil && recipient.PersonID == personID {
				allowed = true
				break
			}
		}
		if allowed {
			break
		}
	}
	if !allowed {
		http.Error(w, "Tax form is not associated with this hackathon's payouts", http.StatusForbidden)
		return
	}
	person, err := getters.FetchSpeakerByID(ctx, personID)
	if err != nil || person == nil || strings.TrimSpace(person.TaxFormObjectKey) == "" {
		http.NotFound(w, r)
		return
	}
	envelope, err := spaces.Get(person.TaxFormObjectKey)
	if err != nil {
		ctx.Err.Printf("tax form download %s: %s", personID, err)
		http.Error(w, "Unable to load tax form", http.StatusInternalServerError)
		return
	}
	plain, err := payoutdocs.Decrypt(ctx.Env.TaxFormEncryptionKey, envelope)
	if err != nil {
		ctx.Err.Printf("tax form decrypt %s: %s", personID, err)
		http.Error(w, "Unable to decrypt tax form", http.StatusInternalServerError)
		return
	}
	filename := filepath.Base(person.TaxFormOriginalName)
	filename = strings.NewReplacer("\r", "", "\n", "", `"`, "'").Replace(filename)
	if strings.TrimSpace(filename) == "" || filename == "." {
		filename = "tax-form"
	}
	contentType := "application/octet-stream"
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		contentType = "application/pdf"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(plain)
}

func HackathonAdminCreateAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/awards")
	in, err := awardInputFromRequest(w, r, competitionID)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if _, err := getters.CreateAward(ctx, in); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards create: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Award added"), http.StatusSeeOther)
}

func HackathonAdminUpdateAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/awards")
	in, err := awardInputFromRequest(w, r, competitionID)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	awardID := strings.TrimSpace(r.FormValue("AwardID"))
	if awardID == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("award is required"), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateAward(ctx, awardID, in); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/update: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Award saved"), http.StatusSeeOther)
}

func HackathonAdminArchiveAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/awards")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	awardID := strings.TrimSpace(r.FormValue("AwardID"))
	if awardID == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("award is required"), http.StatusSeeOther)
		return
	}
	if err := getters.ArchiveAward(ctx, competitionID, awardID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/archive: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Award archived"), http.StatusSeeOther)
}

func HackathonAdminRestoreAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/awards")
	awardID, err := awardIDFromRequest(w, r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.RestoreAward(ctx, competitionID, awardID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/restore: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Award restored"), http.StatusSeeOther)
}

func HackathonAdminDeleteArchivedAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/awards")
	awardID, err := awardIDFromRequest(w, r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.DeleteArchivedAward(ctx, competitionID, awardID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/delete: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Award deleted"), http.StatusSeeOther)
}

func HackathonAdminCreatePrize(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/awards")
	in, err := prizeInputFromRequest(w, r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if !awardBelongsToCompetition(ctx, competitionID, in.AwardID) {
		handle404(w, r, ctx)
		return
	}
	if _, err := getters.CreatePrize(ctx, in); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/prizes create: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Prize added"), http.StatusSeeOther)
}

func HackathonAdminUpdatePrize(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/awards")
	in, err := prizeInputFromRequest(w, r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	prizeID := strings.TrimSpace(r.FormValue("PrizeID"))
	if prizeID == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("prize is required"), http.StatusSeeOther)
		return
	}
	if err := getters.UpdatePrize(ctx, competitionID, prizeID, in); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/prizes/update: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Prize saved"), http.StatusSeeOther)
}

func HackathonAdminDeletePrize(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/awards")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	prizeID := strings.TrimSpace(r.FormValue("PrizeID"))
	if prizeID == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("prize is required"), http.StatusSeeOther)
		return
	}
	if err := getters.DeletePrize(ctx, competitionID, prizeID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/prizes/delete: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Prize deleted"), http.StatusSeeOther)
}

func HackathonAdminAssignAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	defaultDest := hackathonAdminRequestURL(r, competitionID, "/awards")
	awardID, projectID, err := awardAssignmentFromRequest(w, r)
	dest := awardAssignmentRedirectURL(r, competitionID, r.FormValue("Source"), defaultDest)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if !awardBelongsToCompetition(ctx, competitionID, awardID) || !projectBelongsToCompetition(ctx, competitionID, projectID) {
		handle404(w, r, ctx)
		return
	}
	if err := getters.AssignProjectAward(ctx, awardID, projectID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/assign: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Award assigned"), http.StatusSeeOther)
}

func awardAssignmentRedirectURL(r *http.Request, competitionID, source, defaultDest string) string {
	switch strings.TrimSpace(source) {
	case "scores":
		return hackathonAdminRequestURL(r, competitionID, "/judging/scores")
	default:
		return defaultDest
	}
}

func HackathonAdminRemoveAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	defaultDest := hackathonAdminRequestURL(r, competitionID, "/awards")
	awardID, projectID, err := awardAssignmentFromRequest(w, r)
	dest := awardAssignmentRedirectURL(r, competitionID, r.FormValue("Source"), defaultDest)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if !awardBelongsToCompetition(ctx, competitionID, awardID) || !projectBelongsToCompetition(ctx, competitionID, projectID) {
		handle404(w, r, ctx)
		return
	}
	if err := getters.RemoveProjectAward(ctx, awardID, projectID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/awards/remove: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Award removed"), http.StatusSeeOther)
}

func HackathonAdminFinalizeResults(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireHackathonAdmin(w, r, ctx)
	if id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonResultsRedirectURL(r, competitionID)
	if id.Speaker == nil || strings.TrimSpace(id.Speaker.ID) == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("A linked profile is required to finalize results"), http.StatusSeeOther)
		return
	}
	if err := getters.FinalizeCompetitionResults(ctx, competitionID, id.Speaker.ID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/results/finalize: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	flash := "Results finalized and winners published"
	if sent, failed := sendFinalizedAwardNotifications(r, ctx, competitionID); failed > 0 {
		flash += fmt.Sprintf("; sent %d award notifications, %d failed", sent, failed)
	} else if sent > 0 {
		flash += fmt.Sprintf("; emailed %d winning team members", sent)
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

func sendFinalizedAwardNotifications(r *http.Request, ctx *config.AppContext, competitionID string) (sent, failed int) {
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil || competition == nil || competition.ResultsFinalizedAt == nil {
		ctx.Err.Printf("hackathon award notifications %s competition: %v", competitionID, err)
		return 0, 1
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil || conf == nil {
		ctx.Err.Printf("hackathon award notifications %s conference: %v", competitionID, err)
		return 0, 1
	}
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("hackathon award notifications %s awards: %s", competitionID, err)
		return 0, 1
	}
	assignments, err := getters.ListProjectAwardsForCompetition(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("hackathon award notifications %s assignments: %s", competitionID, err)
		return 0, 1
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competitionID, types.HackathonViewer{Admin: true})
	if err != nil {
		ctx.Err.Printf("hackathon award notifications %s projects: %s", competitionID, err)
		return 0, 1
	}
	awardByID := make(map[string]*types.Award, len(awards))
	for _, award := range awards {
		if award != nil {
			awardByID[award.ID] = award
		}
	}
	projectByID := make(map[string]*types.HackathonProject, len(projects))
	for _, project := range projects {
		if project != nil {
			projectByID[project.ID] = project
		}
	}
	awardsByProject := map[string][]*types.Award{}
	for _, assignment := range assignments {
		if assignment != nil && awardByID[assignment.AwardID] != nil {
			awardsByProject[assignment.ProjectID] = append(awardsByProject[assignment.ProjectID], awardByID[assignment.AwardID])
		}
	}
	publicURL := absoluteURL(r, "/"+url.PathEscape(conf.Tag)+"/hackathon#awards")
	for projectID, projectAwards := range awardsByProject {
		project := projectByID[projectID]
		if project == nil {
			continue
		}
		members, memberErr := getters.ListProjectMembers(ctx, projectID)
		if memberErr != nil {
			ctx.Err.Printf("hackathon award notifications %s team %s: %s", competitionID, projectID, memberErr)
			failed++
			continue
		}
		for _, member := range members {
			if member == nil || strings.TrimSpace(member.Email) == "" {
				continue
			}
			notification := emails.AwardNotification{Person: member, Project: project, Awards: projectAwards}
			if err := emails.SendAwardNotification(ctx, conf, competition, notification, publicURL); err != nil {
				ctx.Err.Printf("hackathon award notification %s/%s: %s", projectID, member.PersonID, err)
				failed++
			} else {
				sent++
			}
		}
	}
	return sent, failed
}

func HackathonAdminReopenResults(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireHackathonAdmin(w, r, ctx)
	if id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonResultsRedirectURL(r, competitionID)
	if id.Speaker == nil || strings.TrimSpace(id.Speaker.ID) == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("A linked profile is required to reopen results"), http.StatusSeeOther)
		return
	}
	if err := getters.ReopenCompetitionResults(ctx, competitionID, id.Speaker.ID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/results/reopen: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Results reopened; winners are hidden from the public page"), http.StatusSeeOther)
}

func hackathonResultsRedirectURL(r *http.Request, competitionID string) string {
	if strings.TrimSpace(r.FormValue("ReturnTo")) == "awards" {
		return hackathonAdminRequestURL(r, competitionID, "/awards")
	}
	return hackathonAdminRequestURL(r, competitionID, "/judging/scores")
}

func HackathonAdminJudging(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging conf: %s", competitionID, err)
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	judges, err := getters.ListCompetitionJudges(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging judges: %s", competitionID, err)
		http.Error(w, "Unable to load judges", http.StatusInternalServerError)
		return
	}
	judgeEvents, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging events: %s", competitionID, err)
		http.Error(w, "Unable to load judge events", http.StatusInternalServerError)
		return
	}
	judgeEvents = timelineJudgeEvents(judgeEvents)
	judgeInviteLink := strings.TrimSpace(r.URL.Query().Get("invite"))
	judgeInviteQRCodeURI := ""
	if judgeInviteLink != "" {
		if uri, err := qrCodeDataURI(judgeInviteLink, 192); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/judging invite qr: %s", competitionID, err)
		} else {
			judgeInviteQRCodeURI = uri
		}
	}

	page := &HackathonAdminPage{
		Competition:          competition,
		Conf:                 conf,
		ActiveTab:            "judging",
		Judges:               judges,
		JudgeEvents:          judgeEvents,
		JudgeInviteLink:      judgeInviteLink,
		JudgeInviteQRCodeURI: judgeInviteQRCodeURI,
		FlashMessage:         r.URL.Query().Get("flash"),
		FlashError:           r.URL.Query().Get("error"),
		Year:                 helpers.CurrentYear(),
	}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_judging.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminUpdateJudgeEventRanks(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	eventIDs := r.Form["JudgeEventID"]
	rankLimits := r.Form["RankLimit"]
	if len(eventIDs) == 0 {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("No judging events to update"), http.StatusSeeOther)
		return
	}
	if len(rankLimits) != len(eventIDs) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Rank count form is incomplete"), http.StatusSeeOther)
		return
	}
	for i, eventID := range eventIDs {
		eventID = strings.TrimSpace(eventID)
		rankLimit, err := strconv.Atoi(strings.TrimSpace(rankLimits[i]))
		if eventID == "" || err != nil || rankLimit <= 0 {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Rank counts must be positive numbers"), http.StatusSeeOther)
			return
		}
		if err := getters.UpdateJudgeEventRankLimit(ctx, competitionID, eventID, rankLimit); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/judging/events/ranks %s: %s", competitionID, eventID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to save rank counts"), http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Rank counts saved"), http.StatusSeeOther)
}

func HackathonAdminUpdateJudgingMode(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	mode := strings.TrimSpace(r.FormValue("JudgingMode"))
	if err := getters.UpdateCompetitionJudgingMode(ctx, competitionID, mode); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/mode: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to save judging mode"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judging mode saved"), http.StatusSeeOther)
}

func HackathonAdminUpdateJudgeEventState(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	eventID := strings.TrimSpace(r.FormValue("JudgeEventID"))
	state := strings.TrimSpace(r.FormValue("State"))
	if err := getters.UpdateJudgeEventState(ctx, competitionID, eventID, state); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/events/state %s: %s", competitionID, eventID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to update judging event"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judging event updated"), http.StatusSeeOther)
}

func HackathonAdminCreateJudgeEvent(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/timeline")
	http.Redirect(w, r, dest+"?error="+url.QueryEscape("Judging events are created from timeline blocks."), http.StatusSeeOther)
}

func HackathonAdminDeleteJudgeEvent(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/timeline")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	judgeEventID := strings.TrimSpace(r.FormValue("JudgeEventID"))
	if err := getters.DeleteJudgeEvent(ctx, competitionID, judgeEventID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/events/delete: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judge event deleted"), http.StatusSeeOther)
}

func HackathonAdminAddJudge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	judgeTypes, err := judgeTypesFromForm(r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	personIDs := personIDsFromForm(r, "PersonID")
	if len(personIDs) == 0 {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Choose at least one person from the search results"), http.StatusSeeOther)
		return
	}
	if err := validatePersonIDs(ctx, personIDs); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges selected people: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	existingJudges, err := getters.ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges existing: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to check existing judges. Please try again."), http.StatusSeeOther)
		return
	}
	existingByPersonID := make(map[string][]string, len(existingJudges))
	for _, judge := range existingJudges {
		if judge != nil {
			existingByPersonID[judge.PersonID] = judge.JudgeTypes
			if len(judge.JudgeTypes) == 0 && judge.JudgeType != "" {
				existingByPersonID[judge.PersonID] = []string{judge.JudgeType}
			}
		}
	}
	addedCount := 0
	updatedCount := 0
	alreadyCount := 0
	for _, personID := range personIDs {
		if existingTypes, exists := existingByPersonID[personID]; exists {
			if sameJudgeTypes(existingTypes, judgeTypes) {
				alreadyCount++
				continue
			}
			if err := getters.SetCompetitionJudgeTypes(ctx, competitionID, personID, judgeTypes); err != nil {
				ctx.Err.Printf("/admin/hackathons/%s/judging/judges update %s: %s", competitionID, personID, err)
				http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to update selected judge role. Please try again."), http.StatusSeeOther)
				return
			}
			updatedCount++
			continue
		}
		if err := getters.SetCompetitionJudgeTypes(ctx, competitionID, personID, judgeTypes); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/judging/judges add %s: %s", competitionID, personID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to add selected judge. Please try again."), http.StatusSeeOther)
			return
		}
		addedCount++
	}
	message := ""
	if updatedCount > 0 && addedCount == 0 {
		message = fmt.Sprintf("Updated %d judge role", updatedCount)
		if updatedCount != 1 {
			message += "s"
		}
	} else if addedCount > 0 {
		message = fmt.Sprintf("Added %d judge", addedCount)
		if addedCount != 1 {
			message += "s"
		}
		if alreadyCount > 0 {
			message += fmt.Sprintf("; %d already selected", alreadyCount)
		}
		if updatedCount > 0 {
			message += fmt.Sprintf("; updated %d role", updatedCount)
			if updatedCount != 1 {
				message += "s"
			}
		}
	} else if alreadyCount == 1 {
		message = "Selected person is already a judge"
	} else {
		message = "Selected people are already judges"
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(message), http.StatusSeeOther)
}

func HackathonAdminCreateJudgeInvite(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	judgeTypes, err := judgeInviteTypesFromForm(r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	recipientEmail := strings.TrimSpace(r.FormValue("Email"))
	if recipientEmail != "" {
		address, parseErr := mail.ParseAddress(recipientEmail)
		if parseErr != nil || !strings.EqualFold(address.Address, recipientEmail) {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Enter a valid recipient email address"), http.StatusSeeOther)
			return
		}
		recipientEmail = address.Address
	}
	token, invite, err := getters.CreateCompetitionJudgeInvite(ctx, competitionID, recipientEmail, judgeTypes, nil)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges/invites: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	inviteURL := absoluteURL(r, "/hackathons/judge-invites/"+url.PathEscape(token))
	flash := "Judge invite link created"
	if recipientEmail != "" {
		competition, competitionErr := getters.GetCompetitionByID(ctx, competitionID)
		var conf *types.Conf
		if competitionErr == nil && competition != nil {
			conf, competitionErr = getters.GetConfByRef(ctx, competition.ConferenceID)
		}
		if competitionErr != nil || conf == nil {
			ctx.Err.Printf("/admin/hackathons/%s judge invite email context: %v", competitionID, competitionErr)
			flash += "; email could not be prepared"
		} else if err := emails.SendJudgeInvitation(ctx, conf, competition, invite, inviteURL); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s judge invite email to %s: %s", competitionID, recipientEmail, err)
			flash += "; email delivery failed, copy the link manually"
		} else {
			flash += " and emailed to " + recipientEmail
		}
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(flash)+"&invite="+url.QueryEscape(inviteURL), http.StatusSeeOther)
}

func HackathonAdminUpdateJudgeRoles(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	rolesByPersonID, err := judgeRolesFromForm(r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	existingJudges, err := getters.ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges/roles existing: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to load current judge roles. Please try again."), http.StatusSeeOther)
		return
	}
	if len(existingJudges) != len(rolesByPersonID) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("The judge list changed. Refresh the page and try again."), http.StatusSeeOther)
		return
	}
	for _, judge := range existingJudges {
		if judge == nil || rolesByPersonID[judge.PersonID] == nil {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("The judge list changed. Refresh the page and try again."), http.StatusSeeOther)
			return
		}
	}
	if err := getters.SetCompetitionJudgeRoles(ctx, competitionID, rolesByPersonID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges/roles update: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to save judge roles. Please try again."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judge roles saved"), http.StatusSeeOther)
}

func HackathonAdminRemoveJudge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "/judging")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	personID := strings.TrimSpace(r.FormValue("PersonID"))
	if err := getters.RemoveCompetitionJudge(ctx, competitionID, personID, getters.JudgeTypeExpo); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges remove: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judge removed"), http.StatusSeeOther)
}

func HackathonAdminNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireHackathonAdmin(w, r, ctx)
	if id == nil {
		return
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/new list confs: %s", err)
		http.Error(w, "Unable to load conferences", http.StatusInternalServerError)
		return
	}
	setupFromSchedule := r.URL.Query().Get("schedule") == "1"
	competition := &types.HackathonCompetition{Visibility: getters.CompetitionVisibilityHidden}
	selectedConf := hackathonSetupConf(confs, r.URL.Query().Get("conf"))
	if conf := selectedConf; conf != nil {
		existing, err := getters.GetCompetitionByConferenceID(ctx, conf.Ref)
		if err != nil {
			ctx.Err.Printf("/admin/hackathons/new conf %s hackathon lookup: %s", conf.Tag, err)
			http.Error(w, "Unable to load hackathon", http.StatusInternalServerError)
			return
		}
		if existing != nil {
			http.Redirect(w, r, "/admin/hackathons/"+url.PathEscape(existing.ID)+"?setup=1", http.StatusSeeOther)
			return
		}
		competition.ConferenceID = conf.Ref
	}
	page := &HackathonAdminPage{
		Conf:               selectedConf,
		Confs:              availableHackathonConfs(ctx, hackathonAdminConfs(id, confs), ""),
		Competition:        competition,
		IsNew:              true,
		SetupFromSchedule:  setupFromSchedule,
		SetupStep:          1,
		SeedScheduleBlocks: setupFromSchedule,
		FlashMessage:       r.URL.Query().Get("flash"),
		FlashError:         r.URL.Query().Get("error"),
		Year:               helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_detail.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/new template: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	in, err := hackathonCompetitionInputFromRequest(w, r)
	if err != nil {
		http.Redirect(w, r, "/admin/hackathons/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if existing, err := getters.GetCompetitionByConferenceID(ctx, in.ConferenceID); err != nil {
		ctx.Err.Printf("/admin/hackathons create conference lookup: %s", err)
		http.Redirect(w, r, "/admin/hackathons/new?error="+url.QueryEscape("Unable to check conference hackathon"), http.StatusSeeOther)
		return
	} else if existing != nil {
		http.Redirect(w, r, "/admin/hackathons/"+url.PathEscape(existing.ID)+"?setup=1&flash="+url.QueryEscape("That conference already has a hackathon"), http.StatusSeeOther)
		return
	}
	id, err := getters.CreateCompetition(ctx, in)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons create: %s", err)
		http.Redirect(w, r, "/admin/hackathons/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	dest := "/admin/hackathons/" + url.PathEscape(id) + "?setup=2"
	if r.PostFormValue("NextSetupStep") == "3" {
		dest = "/admin/hackathons/" + url.PathEscape(id) + "/awards?setup=3"
	} else if r.PostFormValue("SetupFromSchedule") == "1" {
		dest += "&schedule=1"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func seedCompetitionScheduleBlocks(ctx *config.AppContext, in getters.CompetitionInput) (string, error) {
	if strings.TrimSpace(in.ConferenceID) == "" {
		return "", fmt.Errorf("conference is required")
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		return "", fmt.Errorf("list conferences: %w", err)
	}
	conf := helpers.FindConfByRef(confs, in.ConferenceID)
	if conf == nil {
		return "", fmt.Errorf("conference schedule not found")
	}
	added, err := seedHackathonScheduleProposals(ctx, conf)
	if err != nil {
		return "", err
	}
	return scheduleHackathonSeedFlash(added), nil
}

func handleHackathonSetupStep2(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, competitionID, dest string) bool {
	if r.PostFormValue("SetupStep") != "2" {
		return false
	}
	flash := "Timeline saved"
	segments, err := competitionScheduleSegmentInputsFromRequest(r)
	if err != nil {
		http.Redirect(w, r, dest+"?setup=2&error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return true
	}
	if err := getters.ReplaceCompetitionScheduleSegments(ctx, competitionID, segments); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s setup schedule segments: %s", competitionID, err)
		http.Redirect(w, r, dest+"?setup=2&error="+url.QueryEscape("Timeline saved, but schedule segments could not be saved"), http.StatusSeeOther)
		return true
	}
	switch r.PostFormValue("NextSetupStep") {
	case "1":
		http.Redirect(w, r, dest+"?setup=1&flash="+url.QueryEscape(flash), http.StatusSeeOther)
	case "2":
		http.Redirect(w, r, dest+"?setup=2&flash="+url.QueryEscape(flash), http.StatusSeeOther)
	default:
		http.Redirect(w, r, dest+"/awards?setup=3&flash="+url.QueryEscape(flash), http.StatusSeeOther)
	}
	return true
}

func HackathonAdminEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireHackathonAdmin(w, r, ctx)
	if id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s list confs: %s", competitionID, err)
		http.Error(w, "Unable to load conferences", http.StatusInternalServerError)
		return
	}
	page := &HackathonAdminPage{
		Conf:         helpers.FindConfByRef(confs, competition.ConferenceID),
		Confs:        availableHackathonConfs(ctx, hackathonAdminConfs(id, confs), competition.ID),
		Competition:  competition,
		ActiveTab:    "main",
		SetupStep:    0,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if page.Conf != nil && helpers.FindConfByRef(page.Confs, page.Conf.Ref) == nil {
		page.Confs = append(page.Confs, page.Conf)
	}
	if r.URL.Query().Get("setup") == "2" {
		page.SetupStep = 2
		page.ActiveTab = ""
	} else if r.URL.Query().Get("setup") == "1" {
		page.SetupStep = 1
		page.ActiveTab = ""
	}
	page.ScheduleSegments = loadCompetitionScheduleSegments(ctx, competitionID)
	if page.SetupStep == 2 {
		scheduleEvents, err := loadHackathonScheduleEvents(ctx, competitionID)
		if err != nil {
			ctx.Err.Printf("/admin/hackathons/%s setup schedule events: %s", competitionID, err)
		} else {
			sortScheduleSegmentsByScheduleTime(page.ScheduleSegments, scheduleEventsBySegmentID(scheduleEvents))
		}
	}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_detail.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireHackathonAdmin(w, r, ctx)
	if id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "")
	existingCompetition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil || existingCompetition == nil {
		handle404(w, r, ctx)
		return
	}
	in, err := hackathonCompetitionInputFromRequest(w, r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	targetConf, err := getters.GetConfByRef(ctx, in.ConferenceID)
	if err != nil || targetConf == nil || (in.ConferenceID != existingCompetition.ConferenceID && !id.HasRoleForConf(targetConf.Tag, auth.RoleAdmin)) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("You do not have access to move this hackathon to that conference"), http.StatusSeeOther)
		return
	}
	if existing, err := getters.GetCompetitionByConferenceID(ctx, in.ConferenceID); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s update conference lookup: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to check conference hackathon"), http.StatusSeeOther)
		return
	} else if existing != nil && existing.ID != competitionID {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("That conference already has a hackathon"), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateCompetition(ctx, competitionID, in); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s update: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	dest = "/" + url.PathEscape(targetConf.Tag) + "/admin/hackathon"
	if handleHackathonSetupStep2(w, r, ctx, competitionID, dest) {
		return
	}
	if r.PostFormValue("SetupStep") == "1" {
		switch r.PostFormValue("NextSetupStep") {
		case "1":
			http.Redirect(w, r, dest+"?setup=1&flash="+url.QueryEscape("Basics saved"), http.StatusSeeOther)
		case "3":
			http.Redirect(w, r, dest+"/awards?setup=3&flash="+url.QueryEscape("Basics saved"), http.StatusSeeOther)
		default:
			http.Redirect(w, r, dest+"?setup=2&flash="+url.QueryEscape("Basics saved"), http.StatusSeeOther)
		}
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Hackathon saved"), http.StatusSeeOther)
}

func HackathonAdminUpdateVisibility(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireHackathonAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := hackathonAdminRequestURL(r, competitionID, "")
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	visibility, err := hackathonVisibilityFromForm(r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateCompetitionVisibility(ctx, competitionID, visibility); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s visibility: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to update visibility"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Visibility saved"), http.StatusSeeOther)
}

func hackathonAdminConfs(id *auth.Identity, confs []*types.Conf) []*types.Conf {
	if id == nil || id.IsGlobalAdmin() {
		return confs
	}
	out := make([]*types.Conf, 0, len(confs))
	for _, conf := range confs {
		if conf != nil && id.HasRoleForConf(conf.Tag, auth.RoleAdmin) {
			out = append(out, conf)
		}
	}
	return out
}

func hackathonCompetitionInputFromRequest(w http.ResponseWriter, r *http.Request) (getters.CompetitionInput, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return getters.CompetitionInput{}, fmt.Errorf("bad form")
	}
	visibility, err := hackathonVisibilityFromForm(r)
	if err != nil {
		return getters.CompetitionInput{}, err
	}
	lifecycleOverride, err := hackathonLifecycleOverrideFromForm(r)
	if err != nil {
		return getters.CompetitionInput{}, err
	}
	in := getters.CompetitionInput{
		ConferenceID:         strings.TrimSpace(r.FormValue("ConferenceID")),
		Slug:                 strings.TrimSpace(r.FormValue("Slug")),
		Title:                strings.TrimSpace(r.FormValue("Title")),
		Description:          strings.TrimSpace(r.FormValue("Description")),
		DescriptionFormat:    strings.TrimSpace(r.FormValue("DescriptionFormat")),
		Visibility:           visibility,
		LifecycleOverride:    lifecycleOverride,
		PublicGalleryEnabled: checkboxValue(r, "PublicGalleryEnabled"),
		AllowLateSubmissions: checkboxValue(r, "AllowLateSubmissions"),
		PublicTablesEnabled:  checkboxValue(r, "PublicTablesEnabled"),
	}
	if in.ConferenceID == "" {
		return getters.CompetitionInput{}, fmt.Errorf("conference is required")
	}
	if maxTeamRaw := strings.TrimSpace(r.FormValue("MaxTeamSize")); maxTeamRaw != "" {
		maxTeamSize, err := strconv.Atoi(maxTeamRaw)
		if err != nil || maxTeamSize <= 0 {
			return getters.CompetitionInput{}, fmt.Errorf("max team size must be a positive number")
		}
		in.MaxTeamSize = &maxTeamSize
	}
	if in.SubmissionsOpenAt, err = parseAdminLocalTime(r.FormValue("SubmissionsOpenAt")); err != nil {
		return getters.CompetitionInput{}, fmt.Errorf("submissions open: %w", err)
	}
	if in.SubmissionsCloseAt, err = parseAdminLocalTime(r.FormValue("SubmissionsCloseAt")); err != nil {
		return getters.CompetitionInput{}, fmt.Errorf("submissions close: %w", err)
	}
	if in.PublicGalleryAt, err = parseAdminLocalTime(r.FormValue("PublicGalleryAt")); err != nil {
		return getters.CompetitionInput{}, fmt.Errorf("public gallery: %w", err)
	}
	return in, nil
}

func checkboxValue(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.PostForm.Get(name))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func hackathonLifecycleOverrideFromForm(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.FormValue("LifecycleOverride"))
	switch value {
	case "", getters.CompetitionLifecycleUpcoming, getters.CompetitionLifecycleOpen, getters.CompetitionLifecycleSubmissionsClosed, getters.CompetitionLifecycleClosed:
		return value, nil
	default:
		return "", fmt.Errorf("state override is invalid")
	}
}

func loadCompetitionScheduleSegments(ctx *config.AppContext, competitionID string) []*types.CompetitionScheduleSegment {
	segments, err := getters.ListCompetitionScheduleSegments(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s schedule segments: %s", competitionID, err)
		return nil
	}
	return segments
}

func scheduleEventsBySegmentID(events []HackathonScheduleEvent) map[string]HackathonScheduleEvent {
	byID := make(map[string]HackathonScheduleEvent, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.SegmentID) != "" {
			byID[event.SegmentID] = event
		}
	}
	return byID
}

func sortScheduleSegmentsByScheduleTime(segments []*types.CompetitionScheduleSegment, eventsByID map[string]HackathonScheduleEvent) {
	sort.SliceStable(segments, func(i, j int) bool {
		left := scheduleSegmentSortTime(segments[i], eventsByID)
		right := scheduleSegmentSortTime(segments[j], eventsByID)
		if left != nil && right != nil {
			if !left.Equal(*right) {
				return left.Before(*right)
			}
			return scheduleSegmentSortFallback(segments[i], segments[j])
		}
		if left != nil {
			return true
		}
		if right != nil {
			return false
		}
		return scheduleSegmentSortFallback(segments[i], segments[j])
	})
}

func scheduleSegmentSortTime(segment *types.CompetitionScheduleSegment, eventsByID map[string]HackathonScheduleEvent) *time.Time {
	if segment == nil || eventsByID == nil {
		return nil
	}
	event, ok := eventsByID[segment.ID]
	if !ok {
		return nil
	}
	return event.Time
}

func scheduleSegmentSortFallback(left, right *types.CompetitionScheduleSegment) bool {
	if left == nil {
		return false
	}
	if right == nil {
		return true
	}
	if left.Ordering != right.Ordering {
		return left.Ordering < right.Ordering
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.Before(right.CreatedAt)
	}
	return left.ID < right.ID
}

func competitionScheduleSegmentInputsFromRequest(r *http.Request) ([]getters.CompetitionScheduleSegmentInput, error) {
	ids := r.PostForm["SegmentID"]
	types := r.PostForm["SegmentType"]
	titles := r.PostForm["SegmentTitle"]
	durations := r.PostForm["SegmentDuration"]
	segments := make([]getters.CompetitionScheduleSegmentInput, 0, len(titles))
	for i, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		duration := 30
		if i < len(durations) && strings.TrimSpace(durations[i]) != "" {
			parsed, err := strconv.Atoi(strings.TrimSpace(durations[i]))
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("segment duration must be a positive number")
			}
			duration = parsed
		}
		segmentType := "custom"
		if i < len(types) && strings.TrimSpace(types[i]) != "" {
			segmentType = strings.TrimSpace(types[i])
		}
		id := ""
		if i < len(ids) {
			id = strings.TrimSpace(ids[i])
		}
		segments = append(segments, getters.CompetitionScheduleSegmentInput{
			ID:                     id,
			SegmentType:            segmentType,
			Title:                  title,
			DefaultDurationMinutes: duration,
			Ordering:               len(segments),
		})
	}
	return segments, nil
}

func judgeEventInputFromRequest(w http.ResponseWriter, r *http.Request, competitionID string, loc *time.Location) (getters.JudgeEventInput, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return getters.JudgeEventInput{}, fmt.Errorf("bad form")
	}
	playbookType, err := judgeEventTypeFromForm(r)
	if err != nil {
		return getters.JudgeEventInput{}, err
	}
	ordering := 0
	if raw := strings.TrimSpace(r.FormValue("Ordering")); raw != "" {
		ordering, err = strconv.Atoi(raw)
		if err != nil || ordering < 0 {
			return getters.JudgeEventInput{}, fmt.Errorf("ordering must be zero or greater")
		}
	}
	in := getters.JudgeEventInput{
		CompetitionID: strings.TrimSpace(competitionID),
		Name:          strings.TrimSpace(r.FormValue("Name")),
		PlaybookType:  playbookType,
		Ordering:      ordering,
		RankLimit:     4,
	}
	if in.Name == "" {
		return getters.JudgeEventInput{}, fmt.Errorf("judge event name is required")
	}
	if raw := strings.TrimSpace(r.FormValue("StartingProjectNumber")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return getters.JudgeEventInput{}, fmt.Errorf("starting project number must be positive")
		}
		in.StartingProjectNumber = &n
	}
	if raw := strings.TrimSpace(r.FormValue("RankLimit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return getters.JudgeEventInput{}, fmt.Errorf("rank count must be positive")
		}
		in.RankLimit = n
	}
	if in.StartsAt, err = parseAdminLocalTimeInLocation(r.FormValue("StartsAt"), loc); err != nil {
		return getters.JudgeEventInput{}, fmt.Errorf("starts at: %w", err)
	}
	if in.EndsAt, err = parseAdminLocalTimeInLocation(r.FormValue("EndsAt"), loc); err != nil {
		return getters.JudgeEventInput{}, fmt.Errorf("ends at: %w", err)
	}
	return in, nil
}

func hackathonVisibilityFromForm(r *http.Request) (string, error) {
	visibility := strings.TrimSpace(r.FormValue("Visibility"))
	switch visibility {
	case getters.CompetitionVisibilityHidden, getters.CompetitionVisibilityPublic:
		return visibility, nil
	default:
		return "", fmt.Errorf("visibility must be Hidden or Public")
	}
}

func judgeEventTypeFromForm(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.FormValue("PlaybookType"))
	switch value {
	case getters.JudgeTypeExpo, getters.JudgeTypeFinals:
		return value, nil
	default:
		return "", fmt.Errorf("judge event type must be Expo or Finals")
	}
}

func judgeTypeFromForm(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.FormValue("JudgeType"))
	switch value {
	case getters.JudgeTypeExpo, getters.JudgeTypeFinals, getters.JudgeTypeCoordinator:
		return value, nil
	default:
		return "", fmt.Errorf("judge type must be Expo, Finals, or Coordinator")
	}
}

func judgeTypesFromForm(r *http.Request) ([]string, error) {
	values := r.Form["JudgeType"]
	judgeTypes := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		switch value {
		case getters.JudgeTypeExpo, getters.JudgeTypeFinals, getters.JudgeTypeCoordinator:
		default:
			return nil, fmt.Errorf("judge type must be expo, finals, or coordinator")
		}
		if !seen[value] {
			seen[value] = true
			judgeTypes = append(judgeTypes, value)
		}
	}
	if len(judgeTypes) == 0 {
		return nil, fmt.Errorf("choose at least one judge type")
	}
	return judgeTypes, nil
}

func judgeInviteTypesFromForm(r *http.Request) ([]string, error) {
	values := r.Form["InviteJudgeType"]
	judgeTypes := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		switch value {
		case getters.JudgeTypeExpo, getters.JudgeTypeFinals:
		default:
			return nil, fmt.Errorf("judge invite type must be expo or finals")
		}
		if !seen[value] {
			seen[value] = true
			judgeTypes = append(judgeTypes, value)
		}
	}
	if len(judgeTypes) == 0 {
		return nil, fmt.Errorf("choose at least one judging round")
	}
	return judgeTypes, nil
}

func judgeRolesFromForm(r *http.Request) (map[string][]string, error) {
	personIDs := personIDsFromForm(r, "JudgePersonID")
	if len(personIDs) == 0 {
		return nil, fmt.Errorf("no judges were submitted")
	}
	rolesByPersonID := make(map[string][]string, len(personIDs))
	for _, personID := range personIDs {
		rolesByPersonID[personID] = []string{}
	}
	for _, encoded := range r.Form["JudgeRole"] {
		parts := strings.SplitN(encoded, "|", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid judge role selection")
		}
		personID := strings.TrimSpace(parts[0])
		if _, ok := rolesByPersonID[personID]; !ok {
			return nil, fmt.Errorf("invalid judge role selection")
		}
		judgeType := strings.TrimSpace(parts[1])
		switch judgeType {
		case getters.JudgeTypeExpo, getters.JudgeTypeFinals, getters.JudgeTypeCoordinator:
		default:
			return nil, fmt.Errorf("invalid judge role selection")
		}
		if !containsString(rolesByPersonID[personID], judgeType) {
			rolesByPersonID[personID] = append(rolesByPersonID[personID], judgeType)
		}
	}
	for _, personID := range personIDs {
		if len(rolesByPersonID[personID]) == 0 {
			return nil, fmt.Errorf("choose at least one role for every judge, or remove that judge")
		}
	}
	return rolesByPersonID, nil
}

func sameJudgeTypes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, value := range a {
		set[value] = true
	}
	for _, value := range b {
		if !set[value] {
			return false
		}
	}
	return true
}

func awardBelongsToCompetition(ctx *config.AppContext, competitionID, awardID string) bool {
	competitionID = strings.TrimSpace(competitionID)
	awardID = strings.TrimSpace(awardID)
	if competitionID == "" || awardID == "" {
		return false
	}
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("list awards for competition %s: %s", competitionID, err)
		return false
	}
	for _, award := range awards {
		if award != nil && award.ID == awardID {
			return true
		}
	}
	return false
}

func projectBelongsToCompetition(ctx *config.AppContext, competitionID, projectID string) bool {
	competitionID = strings.TrimSpace(competitionID)
	projectID = strings.TrimSpace(projectID)
	if competitionID == "" || projectID == "" {
		return false
	}
	project, err := getters.GetProjectByID(ctx, projectID)
	if err != nil {
		ctx.Err.Printf("get project %s: %s", projectID, err)
		return false
	}
	return project != nil && project.CompetitionID == competitionID
}

func awardAssignmentFromRequest(w http.ResponseWriter, r *http.Request) (string, string, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return "", "", fmt.Errorf("bad form")
	}
	awardID := strings.TrimSpace(r.FormValue("AwardID"))
	projectID := strings.TrimSpace(r.FormValue("ProjectID"))
	if awardID == "" {
		return "", "", fmt.Errorf("award is required")
	}
	if projectID == "" {
		return "", "", fmt.Errorf("project is required")
	}
	return awardID, projectID, nil
}

func awardIDFromRequest(w http.ResponseWriter, r *http.Request) (string, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return "", fmt.Errorf("bad form")
	}
	awardID := strings.TrimSpace(r.FormValue("AwardID"))
	if awardID == "" {
		return "", fmt.Errorf("award is required")
	}
	return awardID, nil
}

func awardInputFromRequest(w http.ResponseWriter, r *http.Request, competitionID string) (getters.AwardInput, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return getters.AwardInput{}, fmt.Errorf("bad form")
	}
	status, err := awardStatusFromForm(r)
	if err != nil {
		return getters.AwardInput{}, err
	}
	in := getters.AwardInput{
		CompetitionID: strings.TrimSpace(competitionID),
		Title:         strings.TrimSpace(r.FormValue("Title")),
		Description:   strings.TrimSpace(r.FormValue("Description")),
		PhotoURL:      strings.TrimSpace(r.FormValue("PhotoURL")),
		OptInRequired: r.FormValue("OptInRequired") != "",
		FinalistsOnly: r.FormValue("FinalistsOnly") != "",
		Status:        status,
	}
	if in.Title == "" {
		return getters.AwardInput{}, fmt.Errorf("award title is required")
	}
	if raw := strings.TrimSpace(r.FormValue("MaxAwardees")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return getters.AwardInput{}, fmt.Errorf("max awardees must be positive")
		}
		in.MaxAwardees = &n
	}
	return in, nil
}

func prizeInputFromRequest(w http.ResponseWriter, r *http.Request) (getters.PrizeInput, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return getters.PrizeInput{}, fmt.Errorf("bad form")
	}
	prizeType, err := prizeTypeFromForm(r)
	if err != nil {
		return getters.PrizeInput{}, err
	}
	status, err := prizeStatusFromForm(r)
	if err != nil {
		return getters.PrizeInput{}, err
	}
	in := getters.PrizeInput{
		AwardID:     strings.TrimSpace(r.FormValue("AwardID")),
		PrizeType:   prizeType,
		Title:       strings.TrimSpace(r.FormValue("Title")),
		Description: strings.TrimSpace(r.FormValue("Description")),
		ValueText:   strings.TrimSpace(r.FormValue("ValueText")),
		PoolURL:     strings.TrimSpace(r.FormValue("PoolURL")),
		Status:      status,
		Comments:    strings.TrimSpace(r.FormValue("Comments")),
	}
	if in.AwardID == "" {
		return getters.PrizeInput{}, fmt.Errorf("award is required")
	}
	if in.Title == "" {
		return getters.PrizeInput{}, fmt.Errorf("prize title is required")
	}
	value, err := strconv.ParseInt(in.ValueText, 10, 64)
	if err != nil || value <= 0 {
		return getters.PrizeInput{}, fmt.Errorf("prize value must be a positive whole number of satoshis")
	}
	in.ValueText = strconv.FormatInt(value, 10)
	if in.PrizeType == getters.PrizeTypePooled {
		if raw := strings.TrimSpace(r.FormValue("PoolPercentage")); raw != "" {
			n, err := strconv.ParseFloat(raw, 64)
			if err != nil || n < 0 || n > 100 {
				return getters.PrizeInput{}, fmt.Errorf("external pool share must be between 0 and 100 percent")
			}
			in.PoolPercentage = &n
		}
	} else {
		in.PoolURL = ""
	}
	return in, nil
}

func awardStatusFromForm(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.FormValue("Status"))
	switch value {
	case getters.AwardStatusDraft, getters.AwardStatusAvailable, getters.AwardStatusUnawarded, getters.AwardStatusAwarded:
		return value, nil
	default:
		return "", fmt.Errorf("award status is invalid")
	}
}

func prizeTypeFromForm(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.FormValue("PrizeType"))
	switch value {
	case getters.PrizeTypeSats, getters.PrizeTypeInKind, getters.PrizeTypeTickets, getters.PrizeTypePooled, getters.PrizeTypeTrophy:
		return value, nil
	default:
		return "", fmt.Errorf("prize type is invalid")
	}
}

func prizeStatusFromForm(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.FormValue("Status"))
	switch value {
	case getters.PrizeStatusAvailable, getters.PrizeStatusNeedsFunds, getters.PrizeStatusAwarded, getters.PrizeStatusPaid:
		return value, nil
	default:
		return "", fmt.Errorf("prize status is invalid")
	}
}

func hackathonScoreSummaries(projects []*types.HackathonProject, scorecards []*types.Scorecard, events []*types.JudgeEvent) []*HackathonScoreSummary {
	eventByID := make(map[string]*types.JudgeEvent, len(events))
	for _, event := range events {
		if event != nil {
			eventByID[event.ID] = event
		}
	}
	accs := map[string]*scoreSummaryAccumulator{}
	order := make([]*scoreSummaryAccumulator, 0, len(projects))
	for _, project := range projects {
		if project == nil {
			continue
		}
		acc := &scoreSummaryAccumulator{
			events: eventByID,
			summary: &HackathonScoreSummary{
				ProjectID:     project.ID,
				ProjectTitle:  project.Title,
				ProjectNumber: adminProjectNumberLabel(project),
				PointsLabel:   "-",
				RankAverage:   "-",
				sortTitle:     strings.ToLower(project.Title),
			},
		}
		accs[project.ID] = acc
		order = append(order, acc)
	}
	for _, scorecard := range scorecards {
		if scorecard == nil {
			continue
		}
		acc := accs[scorecard.ProjectID]
		if acc == nil {
			acc = &scoreSummaryAccumulator{
				events: eventByID,
				summary: &HackathonScoreSummary{
					ProjectID:     scorecard.ProjectID,
					ProjectTitle:  scorecard.ProjectID,
					ProjectNumber: "TBA",
					PointsLabel:   "-",
					RankAverage:   "-",
					sortTitle:     strings.ToLower(scorecard.ProjectID),
				},
			}
			accs[scorecard.ProjectID] = acc
			order = append(order, acc)
		}
		acc.add(scorecard)
	}
	summaries := make([]*HackathonScoreSummary, 0, len(order))
	for _, acc := range order {
		acc.finish()
		summaries = append(summaries, acc.summary)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		a, b := summaries[i], summaries[j]
		if (a.Points > 0) != (b.Points > 0) {
			return a.Points > 0
		}
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if a.hasRankAverage != b.hasRankAverage {
			return a.hasRankAverage
		}
		if a.hasRankAverage && a.rankAverage != b.rankAverage {
			return a.rankAverage < b.rankAverage
		}
		if a.ScoredScorecards != b.ScoredScorecards {
			return a.ScoredScorecards > b.ScoredScorecards
		}
		return a.sortTitle < b.sortTitle
	})
	return summaries
}

func hackathonAdvancedSelections(projects []*types.HackathonProject, scorecards []*types.Scorecard, events []*types.JudgeEvent, mode string, topN int) []*types.HackathonProject {
	if topN <= 0 {
		return nil
	}
	filteredScorecards := filterHackathonScorecardsByMode(scorecards, events, mode)
	return hackathonAdvancedSelectionsFromScorecards(projects, filteredScorecards, events, firstJudgeEventForMode(events, mode), topN)
}

func hackathonAdvancedSelectionsFromScorecards(projects []*types.HackathonProject, scorecards []*types.Scorecard, events []*types.JudgeEvent, event *types.JudgeEvent, topN int) []*types.HackathonProject {
	if topN <= 0 {
		return nil
	}
	projectByID := make(map[string]*types.HackathonProject, len(projects))
	var eligibleProjects []*types.HackathonProject
	for _, project := range projects {
		if project == nil {
			continue
		}
		projectByID[project.ID] = project
		if projectEligibleForJudgeEvent(project, events, event) {
			eligibleProjects = append(eligibleProjects, project)
		}
	}
	summaries := hackathonScoreSummaries(eligibleProjects, scorecards, events)
	advanced := make([]*types.HackathonProject, 0, min(topN, len(summaries)))
	for _, summary := range summaries {
		if summary == nil || summary.ScoredScorecards == 0 {
			continue
		}
		project := projectByID[summary.ProjectID]
		if project == nil || !projectEligibleForJudgeEvent(project, events, event) {
			continue
		}
		advanced = append(advanced, project)
		if len(advanced) >= topN {
			break
		}
	}
	return advanced
}

func selectedScoreJudgeEventID(competition *types.HackathonCompetition, events []*types.JudgeEvent, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		for _, event := range events {
			if event != nil && event.ID == requested {
				return requested
			}
		}
	}
	if current := currentJudgeEvents(competition, events, time.Now()); len(current) > 0 && current[0] != nil {
		return current[0].ID
	}
	for _, event := range events {
		if event != nil {
			return event.ID
		}
	}
	return ""
}

func filterHackathonScorecardsByJudgeEvent(scorecards []*types.Scorecard, eventID string) []*types.Scorecard {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}
	filtered := make([]*types.Scorecard, 0, len(scorecards))
	for _, scorecard := range scorecards {
		if scorecard == nil || scorecard.JudgeEventID != eventID {
			continue
		}
		filtered = append(filtered, scorecard)
	}
	return filtered
}

func judgeEventDisplayName(event *types.JudgeEvent) string {
	if event == nil || strings.TrimSpace(event.Name) == "" {
		return "selected judging event"
	}
	return event.Name
}

func appendAdminScoresMessage(dest, key, message string) string {
	sep := "?"
	if strings.Contains(dest, "?") {
		sep = "&"
	}
	return dest + sep + url.QueryEscape(key) + "=" + url.QueryEscape(message)
}

func scorecardJudgePeopleByID(ctx *config.AppContext, scorecards []*types.Scorecard) (map[string]*types.Speaker, error) {
	people := make(map[string]*types.Speaker)
	for _, scorecard := range scorecards {
		if scorecard == nil || strings.TrimSpace(scorecard.JudgePersonID) == "" {
			continue
		}
		if _, ok := people[scorecard.JudgePersonID]; ok {
			continue
		}
		person, err := getters.FetchSpeakerByID(ctx, scorecard.JudgePersonID)
		if err != nil {
			return people, err
		}
		people[scorecard.JudgePersonID] = person
	}
	return people, nil
}

func emptyJudgeBallotRanks(limit int) []HackathonJudgeBallotRank {
	if limit <= 0 {
		return nil
	}
	ranks := make([]HackathonJudgeBallotRank, limit)
	for i := range ranks {
		rank := i + 1
		ranks[i] = HackathonJudgeBallotRank{
			Rank:      rank,
			RankLabel: ordinal(rank),
		}
	}
	return ranks
}

func filterHackathonScorecardsByMode(scorecards []*types.Scorecard, events []*types.JudgeEvent, mode string) []*types.Scorecard {
	mode = normalizeHackathonScoreMode(mode)
	if mode == hackathonScoreModeAll {
		return scorecards
	}
	eventMode := make(map[string]string, len(events))
	for _, event := range events {
		if event != nil {
			eventMode[event.ID] = event.PlaybookType
		}
	}
	filtered := make([]*types.Scorecard, 0, len(scorecards))
	for _, scorecard := range scorecards {
		if scorecard == nil || eventMode[scorecard.JudgeEventID] != mode {
			continue
		}
		filtered = append(filtered, scorecard)
	}
	return filtered
}

func firstJudgeEventForMode(events []*types.JudgeEvent, mode string) *types.JudgeEvent {
	mode = normalizeHackathonScoreMode(mode)
	for _, event := range events {
		if event == nil {
			continue
		}
		if mode == hackathonScoreModeAll || event.PlaybookType == mode {
			return event
		}
	}
	return nil
}

func projectsForJudgeEvents(projects []*types.HackathonProject, events []*types.JudgeEvent, selectedEvents []*types.JudgeEvent) []*types.HackathonProject {
	if len(selectedEvents) == 0 {
		return nil
	}
	return projectsForJudgeEvent(projects, events, selectedEvents[0].ID)
}

func projectsForJudgeEvent(projects []*types.HackathonProject, events []*types.JudgeEvent, eventID string) []*types.HackathonProject {
	event := judgeEventByID(events, eventID)
	if event == nil {
		return nil
	}
	filtered := make([]*types.HackathonProject, 0, len(projects))
	for _, project := range projects {
		if projectEligibleForJudgeEvent(project, events, event) {
			filtered = append(filtered, project)
		}
	}
	return filtered
}

func projectEligibleForJudgeEvent(project *types.HackathonProject, events []*types.JudgeEvent, event *types.JudgeEvent) bool {
	if project == nil {
		return false
	}
	if event == nil {
		return false
	}
	if judgeEventIsFirst(events, event.ID) {
		return project.Status == getters.ProjectStatusSubmitted || project.Status == getters.ProjectStatusAdvanced
	}
	return project.Status == getters.ProjectStatusAdvanced
}

func judgeEventIsFirst(events []*types.JudgeEvent, eventID string) bool {
	eventID = strings.TrimSpace(eventID)
	for _, event := range events {
		if event == nil {
			continue
		}
		return event.ID == eventID
	}
	return false
}

func normalizeHackathonScoreMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", hackathonScoreModeAll:
		return hackathonScoreModeAll
	case hackathonScoreModeExpo:
		return hackathonScoreModeExpo
	case hackathonScoreModeFinals:
		return hackathonScoreModeFinals
	default:
		return ""
	}
}

func scoreModeLabel(mode string) string {
	switch normalizeHackathonScoreMode(mode) {
	case hackathonScoreModeExpo:
		return "Expo scorecards"
	case hackathonScoreModeFinals:
		return "Finals scorecards"
	default:
		return "All scorecards"
	}
}

func (a *scoreSummaryAccumulator) add(scorecard *types.Scorecard) {
	a.summary.Scorecards++
	if scorecard.Rank != nil {
		points := rankPoints(a.events[scorecard.JudgeEventID], *scorecard.Rank)
		if points <= 0 {
			return
		}
		a.summary.Points += points
		a.summary.ScoredScorecards++
		a.rankSum += *scorecard.Rank
		a.rankCount++
	}
}

func (a *scoreSummaryAccumulator) finish() {
	s := a.summary
	if s.Points > 0 {
		s.PointsLabel = strconv.Itoa(s.Points)
	}
	if a.rankCount > 0 {
		s.rankAverage = float64(a.rankSum) / float64(a.rankCount)
		s.RankAverage = formatScoreAverage(s.rankAverage)
		s.hasRankAverage = true
	}
}

func judgeEventRankLimit(event *types.JudgeEvent) int {
	if event == nil || event.RankLimit <= 0 {
		return 4
	}
	return event.RankLimit
}

func rankPoints(event *types.JudgeEvent, rank int) int {
	if rank <= 0 {
		return 0
	}
	limit := judgeEventRankLimit(event)
	if rank > limit {
		return 0
	}
	return limit - rank + 1
}

func competitionJudgingMode(competition *types.HackathonCompetition) string {
	if competition != nil && strings.TrimSpace(competition.JudgingMode) == getters.CompetitionJudgingModeManual {
		return getters.CompetitionJudgingModeManual
	}
	return getters.CompetitionJudgingModeAutomatic
}

func judgingModeLabel(competition *types.HackathonCompetition) string {
	if competitionJudgingMode(competition) == getters.CompetitionJudgingModeAutomatic {
		return "Automatic"
	}
	return "Manual"
}

func judgeEventState(event *types.JudgeEvent) string {
	if event == nil {
		return getters.JudgeEventStatePending
	}
	switch strings.TrimSpace(event.State) {
	case getters.JudgeEventStateOpen:
		return getters.JudgeEventStateOpen
	case getters.JudgeEventStateClosed:
		return getters.JudgeEventStateClosed
	default:
		return getters.JudgeEventStatePending
	}
}

func judgeEventStateLabel(event *types.JudgeEvent) string {
	return judgeEventStateValueLabel(judgeEventState(event))
}

func judgeEventStateValueLabel(state string) string {
	switch state {
	case getters.JudgeEventStateOpen:
		return "Open"
	case getters.JudgeEventStateClosed:
		return "Closed"
	default:
		return "Pending"
	}
}

func judgeEventEffectiveState(competition *types.HackathonCompetition, event *types.JudgeEvent, now time.Time) string {
	state := judgeEventState(event)
	if competitionJudgingMode(competition) != getters.CompetitionJudgingModeAutomatic {
		return state
	}
	if event == nil || event.StartsAt == nil {
		return getters.JudgeEventStatePending
	}
	if now.Before(*event.StartsAt) {
		return getters.JudgeEventStatePending
	}
	if event.EndsAt == nil || now.Before(*event.EndsAt) || now.Equal(*event.EndsAt) {
		return getters.JudgeEventStateOpen
	}
	return getters.JudgeEventStateClosed
}

func judgeEventEffectiveStateLabel(competition *types.HackathonCompetition, event *types.JudgeEvent, now time.Time) string {
	return judgeEventStateValueLabel(judgeEventEffectiveState(competition, event, now))
}

func judgeEventAcceptsScores(competition *types.HackathonCompetition, event *types.JudgeEvent, now time.Time) bool {
	return judgeEventEffectiveState(competition, event, now) == getters.JudgeEventStateOpen
}

func currentJudgeEvents(competition *types.HackathonCompetition, events []*types.JudgeEvent, now time.Time) []*types.JudgeEvent {
	for _, event := range events {
		if judgeEventAcceptsScores(competition, event, now) {
			return []*types.JudgeEvent{event}
		}
	}
	return nil
}

func ordinal(n int) string {
	if n%100 >= 11 && n%100 <= 13 {
		return strconv.Itoa(n) + "th"
	}
	switch n % 10 {
	case 1:
		return strconv.Itoa(n) + "st"
	case 2:
		return strconv.Itoa(n) + "nd"
	case 3:
		return strconv.Itoa(n) + "rd"
	default:
		return strconv.Itoa(n) + "th"
	}
}

func adminProjectNumberLabel(project *types.HackathonProject) string {
	if project == nil || project.ProjectNumber == nil {
		return "TBA"
	}
	return strconv.Itoa(*project.ProjectNumber)
}

func formatScoreAverage(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}

func parseAdminLocalTime(value string) (*time.Time, error) {
	return parseAdminLocalTimeInLocation(value, time.Local)
}

func parseAdminLocalTimeInLocation(value string, loc *time.Location) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if loc == nil {
		loc = time.Local
	}
	for _, layout := range []string{"2006-01-02T15:04", time.RFC3339} {
		t, err := time.ParseInLocation(layout, value, loc)
		if err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("invalid date/time")
}
