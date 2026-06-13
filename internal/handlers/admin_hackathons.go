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
	Competitions   []*types.HackathonCompetition
	Confs          []*types.Conf
	Competition    *types.HackathonCompetition
	Projects       []*types.HackathonProject
	ProjectTeams   map[string][]*types.ProjectMember
	JudgeEvents    []*types.JudgeEvent
	Judges         []*types.CompetitionJudge
	Scorecards     []*types.Scorecard
	ScoreSummaries []*HackathonScoreSummary
	IsNew          bool
	FlashMessage   string
	FlashError     string
	Year           uint
}

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
		if conf.Tag != "" && conf.Desc != "" {
			return conf.Tag + " - " + conf.Desc
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

func HackathonAdminList(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
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
	page := &HackathonAdminPage{
		Competitions: competitions,
		Confs:        confs,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
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
	page := &HackathonAdminPage{
		Competition:  competition,
		Projects:     projects,
		ProjectTeams: teams,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_projects.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/projects template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
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
	page := &HackathonAdminPage{
		Competition:    competition,
		Projects:       projects,
		JudgeEvents:    events,
		Judges:         judges,
		Scorecards:     scorecards,
		ScoreSummaries: hackathonScoreSummaries(projects, scorecards),
		FlashMessage:   r.URL.Query().Get("flash"),
		FlashError:     r.URL.Query().Get("error"),
		Year:           helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_scores.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/hackathons/%s/judging/scores template: %s", competitionID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
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
		JudgeEvents:  events,
		Judges:       judges,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
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
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
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
