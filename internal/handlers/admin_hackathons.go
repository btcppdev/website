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
	Confs                []*types.Conf
	Competition          *types.HackathonCompetition
	Projects             []*types.HackathonProject
	ProjectTeams         map[string][]*types.ProjectMember
	ActiveTab            string
	JudgeEvents          []*types.JudgeEvent
	Judges               []*types.CompetitionJudge
	Scorecards           []*types.Scorecard
	ScoreSummaries       []*HackathonScoreSummary
	ScoreMode            string
	Awards               []*types.Award
	ArchivedAwards       []*types.Award
	PrizesByAward        map[string][]*types.Prize
	AwardeesByAward      map[string][]*types.ProjectAward
	AwardOptInsByProject map[string][]*types.ProjectAwardOptIn
	ProjectCount         int
	JudgeEventCount      int
	ScoreProjectCount    int
	AwardCount           int
	IsNew                bool
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
	IdeaAverage      string
	ExecutionAverage string
	ImpactAverage    string
	TotalAverage     string
	RankAverage      string
	BestRank         string

	sortTitle       string
	totalAverage    float64
	rankAverage     float64
	hasTotalAverage bool
	hasRankAverage  bool
}

type scoreSummaryAccumulator struct {
	summary     *HackathonScoreSummary
	ideaSum     int
	ideaCount   int
	execSum     int
	execCount   int
	impactSum   int
	impactCount int
	totalSum    int
	totalCount  int
	rankSum     int
	rankCount   int
	bestRank    int
}

func (p *HackathonAdminPage) ConfLabel(confID string) string {
	confID = strings.TrimSpace(confID)
	if confID == "" {
		return "Standalone"
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

func (p *HackathonAdminPage) EditURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID)
}

func (p *HackathonAdminPage) ProjectsURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID) + "/projects"
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
	return hackathonURL(competition)
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
	return hackathonScheduleURL(competition)
}

func (p *HackathonAdminPage) ProjectPublicURL(project *types.HackathonProject) string {
	if p == nil || p.Competition == nil || project == nil {
		return "/hackathons"
	}
	return hackathonURL(p.Competition) + "#project-" + url.PathEscape(project.ID)
}

func (p *HackathonAdminPage) ProjectManageURL(project *types.HackathonProject) string {
	if p == nil || p.Competition == nil || project == nil {
		return "/hackathons"
	}
	return hackathonURL(p.Competition) + "/projects/" + url.PathEscape(project.ID)
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
	for _, event := range p.JudgeEvents {
		if event != nil && event.ID == eventID {
			return event.Name
		}
	}
	return eventID
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

func (p *HackathonAdminPage) ScoreTotal(scorecard *types.Scorecard) string {
	if scorecard == nil || scorecard.NoShow {
		return "-"
	}
	total := 0
	count := 0
	for _, value := range []*int{scorecard.IdeaScore, scorecard.ExecutionScore, scorecard.ImpactScore} {
		if value != nil {
			total += *value
			count++
		}
	}
	if count == 0 {
		return "-"
	}
	return strconv.Itoa(total)
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

	events := page.JudgeEvents
	if events == nil {
		var err error
		events, err = getters.ListJudgeEvents(ctx, competitionID)
		if err != nil {
			ctx.Err.Printf("/admin/hackathons/%s count judge events: %s", competitionID, err)
		}
	}
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
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_projects.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
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
		Projects:       projects,
		JudgeEvents:    events,
		Judges:         judges,
		Scorecards:     scorecards,
		ScoreSummaries: hackathonScoreSummaries(projects, scorecards),
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
	events, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging events: %s", competitionID, err)
		http.Error(w, "Unable to load judge events", http.StatusInternalServerError)
		return
	}
	judges, err := getters.ListCompetitionJudges(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging judges: %s", competitionID, err)
		http.Error(w, "Unable to load judges", http.StatusInternalServerError)
		return
	}
	page := &HackathonAdminPage{
		Competition:  competition,
		ActiveTab:    "judging",
		JudgeEvents:  events,
		Judges:       judges,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	populateAdminHackathonCounts(ctx, page)
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_judging.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonAdminCreateJudgeEvent(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	dest := "/admin/hackathons/" + url.PathEscape(competitionID) + "/judging"
	in, err := judgeEventInputFromRequest(w, r, competitionID)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if _, err := getters.CreateJudgeEvent(ctx, in); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/events create: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judge event added"), http.StatusSeeOther)
}

func HackathonAdminDeleteJudgeEvent(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
	email := strings.TrimSpace(r.FormValue("Email"))
	personID, err := getters.GetPersonIDByEmail(ctx, email)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("No person found for "+email), http.StatusSeeOther)
		return
	}
	judgeType, err := judgeTypeFromForm(r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.AddCompetitionJudge(ctx, competitionID, personID, judgeType); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/judges add: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Judge added"), http.StatusSeeOther)
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
	page := &HackathonAdminPage{
		Confs:        confs,
		Competition:  &types.HackathonCompetition{Visibility: getters.CompetitionVisibilityHidden},
		IsNew:        true,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
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
	id, err := getters.CreateCompetition(ctx, in)
	if err != nil {
		ctx.Err.Printf("/admin/hackathons create: %s", err)
		http.Redirect(w, r, "/admin/hackathons/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/hackathons/"+url.PathEscape(id)+"?flash="+url.QueryEscape("Hackathon created"), http.StatusSeeOther)
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
		Confs:        confs,
		Competition:  competition,
		ActiveTab:    "main",
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
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
	if err := getters.UpdateCompetition(ctx, competitionID, in); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s update: %s", competitionID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
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
	in := getters.CompetitionInput{
		ConferenceID: strings.TrimSpace(r.FormValue("ConferenceID")),
		Slug:         strings.TrimSpace(r.FormValue("Slug")),
		Title:        strings.TrimSpace(r.FormValue("Title")),
		Description:  strings.TrimSpace(r.FormValue("Description")),
		Visibility:   visibility,
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

func judgeEventInputFromRequest(w http.ResponseWriter, r *http.Request, competitionID string) (getters.JudgeEventInput, error) {
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
	if in.StartsAt, err = parseAdminLocalTime(r.FormValue("StartsAt")); err != nil {
		return getters.JudgeEventInput{}, fmt.Errorf("starts at: %w", err)
	}
	if in.EndsAt, err = parseAdminLocalTime(r.FormValue("EndsAt")); err != nil {
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

func hackathonScoreSummaries(projects []*types.HackathonProject, scorecards []*types.Scorecard) []*HackathonScoreSummary {
	accs := map[string]*scoreSummaryAccumulator{}
	order := make([]*scoreSummaryAccumulator, 0, len(projects))
	for _, project := range projects {
		if project == nil {
			continue
		}
		acc := &scoreSummaryAccumulator{
			summary: &HackathonScoreSummary{
				ProjectID:        project.ID,
				ProjectTitle:     project.Title,
				ProjectNumber:    adminProjectNumberLabel(project),
				IdeaAverage:      "-",
				ExecutionAverage: "-",
				ImpactAverage:    "-",
				TotalAverage:     "-",
				RankAverage:      "-",
				BestRank:         "-",
				sortTitle:        strings.ToLower(project.Title),
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
				summary: &HackathonScoreSummary{
					ProjectID:        scorecard.ProjectID,
					ProjectTitle:     scorecard.ProjectID,
					ProjectNumber:    "TBA",
					IdeaAverage:      "-",
					ExecutionAverage: "-",
					ImpactAverage:    "-",
					TotalAverage:     "-",
					RankAverage:      "-",
					BestRank:         "-",
					sortTitle:        strings.ToLower(scorecard.ProjectID),
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
		if a.hasTotalAverage != b.hasTotalAverage {
			return a.hasTotalAverage
		}
		if a.hasTotalAverage && a.totalAverage != b.totalAverage {
			return a.totalAverage > b.totalAverage
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
	summaries := hackathonScoreSummaries(eligibleProjects, filteredScorecards)
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
	total := 0
	totalParts := 0
	if scorecard.IdeaScore != nil {
		a.ideaSum += *scorecard.IdeaScore
		a.ideaCount++
		total += *scorecard.IdeaScore
		totalParts++
	}
	if scorecard.ExecutionScore != nil {
		a.execSum += *scorecard.ExecutionScore
		a.execCount++
		total += *scorecard.ExecutionScore
		totalParts++
	}
	if scorecard.ImpactScore != nil {
		a.impactSum += *scorecard.ImpactScore
		a.impactCount++
		total += *scorecard.ImpactScore
		totalParts++
	}
	if totalParts > 0 {
		a.summary.ScoredScorecards++
		a.totalSum += total
		a.totalCount++
	}
	if scorecard.Rank != nil {
		a.rankSum += *scorecard.Rank
		a.rankCount++
		if a.bestRank == 0 || *scorecard.Rank < a.bestRank {
			a.bestRank = *scorecard.Rank
		}
	}
}

func (a *scoreSummaryAccumulator) finish() {
	s := a.summary
	if a.ideaCount > 0 {
		s.IdeaAverage = formatScoreAverage(float64(a.ideaSum) / float64(a.ideaCount))
	}
	if a.execCount > 0 {
		s.ExecutionAverage = formatScoreAverage(float64(a.execSum) / float64(a.execCount))
	}
	if a.impactCount > 0 {
		s.ImpactAverage = formatScoreAverage(float64(a.impactSum) / float64(a.impactCount))
	}
	if a.totalCount > 0 {
		s.totalAverage = float64(a.totalSum) / float64(a.totalCount)
		s.TotalAverage = formatScoreAverage(s.totalAverage)
		s.hasTotalAverage = true
	}
	if a.rankCount > 0 {
		s.rankAverage = float64(a.rankSum) / float64(a.rankCount)
		s.RankAverage = formatScoreAverage(s.rankAverage)
		s.BestRank = strconv.Itoa(a.bestRank)
		s.hasRankAverage = true
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
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02T15:04", time.RFC3339} {
		t, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("invalid date/time")
}
