package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

type HackathonAdminPage struct {
	Competitions         []*types.HackathonCompetition
	Conf                 *types.Conf
	Confs                []*types.Conf
	Competition          *types.HackathonCompetition
	Projects             []*types.HackathonProject
	ProjectTeams         map[string][]*types.ProjectMember
	ActiveTab            string
	JudgeEvents          []*types.JudgeEvent
	Judges               []*types.CompetitionJudge
	JudgeInviteLink      string
	JudgeInviteQRCodeURI string
	Scorecards           []*types.Scorecard
	ScoreSummaries       []*HackathonScoreSummary
	ScoreMode            string
	Awards               []*types.Award
	ArchivedAwards       []*types.Award
	PrizesByAward        map[string][]*types.Prize
	AwardeesByAward      map[string][]*types.ProjectAward
	AwardOptInsByProject map[string][]*types.ProjectAwardOptIn
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
	NoShows          int
	Points           int
	PointsLabel      string
	RankAverage      string
	BestRank         string

	sortTitle      string
	rankAverage    float64
	hasRankAverage bool
}

type scoreSummaryAccumulator struct {
	summary   *HackathonScoreSummary
	events    map[string]*types.JudgeEvent
	rankSum   int
	rankCount int
	bestRank  int
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
	return "/admin/hackathons/" + url.PathEscape(p.Competition.ID) + "?setup=1"
}

func (p *HackathonAdminPage) SetupStep2URL() string {
	if !p.SetupStep1Complete() {
		return ""
	}
	return "/admin/hackathons/" + url.PathEscape(p.Competition.ID) + "?setup=2"
}

func (p *HackathonAdminPage) SetupStep3URL() string {
	if !p.SetupStep1Complete() {
		return ""
	}
	return "/admin/hackathons/" + url.PathEscape(p.Competition.ID) + "/awards?setup=3"
}

func (p *HackathonAdminPage) EditURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID)
}

func (p *HackathonAdminPage) SetupURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID) + "?setup=1"
}

func (p *HackathonAdminPage) ProjectsURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID) + "/projects"
}

func (p *HackathonAdminPage) TimelineURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID) + "/timeline"
}

func (p *HackathonAdminPage) JudgingURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID) + "/judging"
}

func (p *HackathonAdminPage) ScoreReviewURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID) + "/judging/scores"
}

func (p *HackathonAdminPage) AwardsURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID) + "/awards"
}

func (p *HackathonAdminPage) HackathonURL(competition *types.HackathonCompetition) string {
	return hackathonURLForConf(p.confForCompetition(competition))
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
	return "/admin/hackathons/" + url.PathEscape(competition.ID) + "/visibility"
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
	return hackathonURLForConf(p.confForCompetition(p.Competition)) + "#project-" + url.PathEscape(project.ID)
}

func (p *HackathonAdminPage) ProjectManageURL(project *types.HackathonProject) string {
	if p == nil || p.Competition == nil || project == nil {
		return ""
	}
	return hackathonURLForConf(p.confForCompetition(p.Competition)) + "/projects/" + url.PathEscape(project.ID)
}

func (p *HackathonAdminPage) ProjectAdminURL(project *types.HackathonProject) string {
	if p == nil || p.Competition == nil || project == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(p.Competition.ID) + "/projects/" + url.PathEscape(project.ID)
}

func (p *HackathonAdminPage) AssignProjectNumbersURL() string {
	if p == nil || p.Competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(p.Competition.ID) + "/projects/assign-numbers"
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

func (p *HackathonAdminPage) RankLimit(event *types.JudgeEvent) int {
	return judgeEventRankLimit(event)
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

func (p *HackathonAdminPage) ScorecardPoints(scorecard *types.Scorecard) string {
	if scorecard == nil || scorecard.NoShow {
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/projects"
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	vars := mux.Vars(r)
	competitionID := vars["competitionID"]
	projectID := vars["projectID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/projects"
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	projectNumber, err := optionalIntFromForm(r, "ProjectNumber", "project number", 1, 0)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateProjectAdminFields(ctx, competitionID, projectID, r.FormValue("Status"), projectNumber); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects/%s update: %s", competitionID, projectID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project updated"), http.StatusSeeOther)
}

func adminProjectInputFromRequest(w http.ResponseWriter, r *http.Request, competitionID string) (getters.ProjectInput, string, *int, error) {
	in, err := projectInputFromRequest(w, r, competitionID)
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/projects"
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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
	awards, err := getters.ListAwardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores awards: %s", competitionID, err)
		http.Error(w, "Unable to load awards", http.StatusInternalServerError)
		return
	}
	page := &HackathonAdminPage{
		Competition:    competition,
		Conf:           conf,
		Projects:       projects,
		JudgeEvents:    events,
		Judges:         judges,
		Scorecards:     scorecards,
		ScoreSummaries: hackathonScoreSummaries(projects, scorecards, events),
		Awards:         awards,
		ActiveTab:      "scores",
		ScoreMode:      hackathonScoreModeAll,
		FlashMessage:   r.URL.Query().Get("flash"),
		FlashError:     r.URL.Query().Get("error"),
		Year:           helpers.CurrentYear(),
	}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_scores.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminTimeline(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/timeline"
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

func HackathonAdminAdvanceFinalists(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/judging/scores"
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	topN, err := optionalIntFromForm(r, "TopN", "finalist count", 1, 0)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if topN == nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("finalist count is required"), http.StatusSeeOther)
		return
	}
	mode := normalizeHackathonScoreMode(r.FormValue("ScoreMode"))
	if mode == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("scoring mode is invalid"), http.StatusSeeOther)
		return
	}
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, types.HackathonViewer{Admin: true})
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/finalists projects: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to load projects"), http.StatusSeeOther)
		return
	}
	events, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/finalists events: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to load judge events"), http.StatusSeeOther)
		return
	}
	events = timelineJudgeEvents(events)
	scorecards, err := getters.ListScorecardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/finalists scorecards: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to load scorecards"), http.StatusSeeOther)
		return
	}
	finalists := hackathonFinalistSelections(projects, scorecards, events, mode, *topN)
	if len(finalists) == 0 {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("No scored eligible projects found for "+scoreModeLabel(mode)+"."), http.StatusSeeOther)
		return
	}
	for _, project := range finalists {
		if project == nil {
			continue
		}
		if err := getters.UpdateProjectAdminFields(ctx, competition.ID, project.ID, getters.ProjectStatusFinalist, project.ProjectNumber); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/judging/finalists project %s: %s", competitionID, project.ID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
	}
	message := fmt.Sprintf("Advanced %d finalist", len(finalists))
	if len(finalists) != 1 {
		message += "s"
	}
	message += " using " + scoreModeLabel(mode)
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(message), http.StatusSeeOther)
}

func HackathonAdminAwards(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil {
		handle404(w, r, ctx)
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

func HackathonAdminCreateAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/awards"
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/awards"
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/awards"
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/awards"
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/awards"
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/awards"
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

func HackathonAdminAssignAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	defaultDest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/awards"
	awardID, projectID, err := awardAssignmentFromRequest(w, r)
	dest := awardAssignmentRedirectURL(competitionID, r.FormValue("Source"), defaultDest)
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

func awardAssignmentRedirectURL(competitionID, source, defaultDest string) string {
	switch strings.TrimSpace(source) {
	case "scores":
		return "/admin/hackathons/" + url.PathEscape(competitionID) + "/judging/scores"
	default:
		return defaultDest
	}
}

func HackathonAdminRemoveAward(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/awards"
	awardID, projectID, err := awardAssignmentFromRequest(w, r)
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

func HackathonAdminJudging(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/judging"
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

func HackathonAdminCreateJudgeEvent(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/timeline"
	http.Redirect(w, r, dest+"?error="+url.QueryEscape("Judging events are created from timeline blocks."), http.StatusSeeOther)
}

func HackathonAdminDeleteJudgeEvent(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/timeline"
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/judging"
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
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
	existingByPersonID := make(map[string]bool, len(existingJudges))
	for _, judge := range existingJudges {
		if judge != nil {
			existingByPersonID[judge.PersonID] = true
		}
	}
	addedCount := 0
	alreadyCount := 0
	for _, personID := range personIDs {
		if existingByPersonID[personID] {
			alreadyCount++
			continue
		}
		if err := getters.AddCompetitionJudge(ctx, competitionID, personID, getters.JudgeTypeCoordinator); err != nil {
			ctx.Err.Printf("/admin/hackathons/%s/judging/judges add %s: %s", competitionID, personID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to add selected judge. Please try again."), http.StatusSeeOther)
			return
		}
		addedCount++
	}
	message := ""
	if addedCount > 0 {
		message = fmt.Sprintf("Added %d judge", addedCount)
		if addedCount != 1 {
			message += "s"
		}
		if alreadyCount > 0 {
			message += fmt.Sprintf("; %d already selected", alreadyCount)
		}
	} else if alreadyCount == 1 {
		message = "Selected person is already a judge"
	} else {
		message = "Selected people are already judges"
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape(message), http.StatusSeeOther)
}

func HackathonAdminCreateJudgeInvite(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/judging"
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	token, _, err := getters.CreateCompetitionJudgeInvite(ctx, competitionID, nil)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges/invites: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	inviteURL := absoluteURL(r, "/hackathons/judge-invites/"+url.PathEscape(token))
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judge invite link created")+"&invite="+url.QueryEscape(inviteURL), http.StatusSeeOther)
}

func HackathonAdminRemoveJudge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/judging"
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	personID := strings.TrimSpace(r.FormValue("PersonID"))
	judgeType, err := judgeTypeFromForm(r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.RemoveCompetitionJudge(ctx, competitionID, personID, judgeType); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges remove: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judge removed"), http.StatusSeeOther)
}

func HackathonAdminNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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
	if conf := hackathonSetupConf(confs, r.URL.Query().Get("conf")); conf != nil {
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
		Confs:              availableHackathonConfs(ctx, confs, ""),
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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

func handleHackathonSetupStep2(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, competitionID string, in getters.CompetitionInput) bool {
	if r.PostFormValue("SetupStep") != "2" {
		return false
	}
	dest := "/admin/hackathons/" + url.PathEscape(competitionID)
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
		http.Redirect(w, r, "/admin/hackathons/"+url.PathEscape(competitionID)+"/awards?setup=3&flash="+url.QueryEscape(flash), http.StatusSeeOther)
	}
	return true
}

func HackathonAdminEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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
		Confs:        availableHackathonConfs(ctx, confs, competition.ID),
		Competition:  competition,
		ActiveTab:    "main",
		SetupStep:    0,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
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
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID)
	in, err := hackathonCompetitionInputFromRequest(w, r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
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
	if handleHackathonSetupStep2(w, r, ctx, competitionID, in) {
		return
	}
	if r.PostFormValue("SetupStep") == "1" {
		dest := "/admin/hackathons/" + url.PathEscape(competitionID)
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
	http.Redirect(w, r, "/admin/hackathons?flash="+url.QueryEscape("Hackathon saved"), http.StatusSeeOther)
}

func HackathonAdminUpdateVisibility(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/hackathons?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	visibility, err := hackathonVisibilityFromForm(r)
	if err != nil {
		http.Redirect(w, r, "/admin/hackathons?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateCompetitionVisibility(ctx, competitionID, visibility); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s visibility: %s", competitionID, err)
		http.Redirect(w, r, "/admin/hackathons?error="+url.QueryEscape("Unable to update visibility"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/hackathons?flash="+url.QueryEscape("Visibility saved"), http.StatusSeeOther)
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
	case "", getters.CompetitionLifecycleUpcoming, getters.CompetitionLifecycleOpen, getters.CompetitionLifecycleSubmissionsClosed, getters.CompetitionLifecyclePublicGallery, getters.CompetitionLifecycleClosed:
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
	if raw := strings.TrimSpace(r.FormValue("PoolPercentage")); raw != "" {
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || n < 0 || n > 100 {
			return getters.PrizeInput{}, fmt.Errorf("pool percentage must be between 0 and 100")
		}
		in.PoolPercentage = &n
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
				BestRank:      "-",
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
					BestRank:      "-",
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

func hackathonFinalistSelections(projects []*types.HackathonProject, scorecards []*types.Scorecard, events []*types.JudgeEvent, mode string, topN int) []*types.HackathonProject {
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
		if projectEligibleForFinalist(project) {
			eligibleProjects = append(eligibleProjects, project)
		}
	}
	filteredScorecards := filterHackathonScorecardsByMode(scorecards, events, mode)
	summaries := hackathonScoreSummaries(eligibleProjects, filteredScorecards, events)
	finalists := make([]*types.HackathonProject, 0, min(topN, len(summaries)))
	for _, summary := range summaries {
		if summary == nil || summary.ScoredScorecards == 0 {
			continue
		}
		project := projectByID[summary.ProjectID]
		if project == nil || !projectEligibleForFinalist(project) {
			continue
		}
		finalists = append(finalists, project)
		if len(finalists) >= topN {
			break
		}
	}
	return finalists
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

func projectEligibleForFinalist(project *types.HackathonProject) bool {
	if project == nil {
		return false
	}
	switch project.Status {
	case getters.ProjectStatusSubmitted, getters.ProjectStatusShipped, getters.ProjectStatusFinalist:
		return true
	default:
		return false
	}
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
	if scorecard.NoShow {
		a.summary.NoShows++
		return
	}
	if scorecard.Rank != nil {
		points := rankPoints(a.events[scorecard.JudgeEventID], *scorecard.Rank)
		if points <= 0 {
			return
		}
		a.summary.Points += points
		a.summary.ScoredScorecards++
		a.rankSum += *scorecard.Rank
		a.rankCount++
		if a.bestRank == 0 || *scorecard.Rank < a.bestRank {
			a.bestRank = *scorecard.Rank
		}
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
		s.BestRank = strconv.Itoa(a.bestRank)
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
