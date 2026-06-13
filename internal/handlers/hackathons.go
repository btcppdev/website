package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

type HackathonPage struct {
	Competition   *types.HackathonCompetition
	Conf          *types.Conf
	Projects      []*types.HackathonProject
	Project       *types.HackathonProject
	Members       []*types.ProjectMember
	JudgeEvents   []*types.JudgeEvent
	Viewer        *auth.Identity
	OwnedProjects map[string]bool
	IsNew         bool
	CanCreate     bool
	CanEdit       bool
	CanSubmit     bool
	CanJudge      bool
	InviteLink    string
	FlashMessage  string
	FlashError    string
	Year          uint
}

type HackathonScheduleEvent struct {
	Label string
	Time  *time.Time
}

func (p *HackathonPage) ConferenceLabel() string {
	if p == nil || p.Conf == nil {
		return ""
	}
	if p.Conf.Tag != "" && p.Conf.Desc != "" {
		return p.Conf.Tag + " - " + p.Conf.Desc
	}
	if p.Conf.Desc != "" {
		return p.Conf.Desc
	}
	return p.Conf.Tag
}

func (p *HackathonPage) ProjectTagsCSV() string {
	if p == nil || p.Project == nil {
		return ""
	}
	return strings.Join(p.Project.Tags, ", ")
}

func (p *HackathonPage) CanManageProject(projectID string) bool {
	return p != nil && p.OwnedProjects != nil && p.OwnedProjects[projectID]
}

func (p *HackathonPage) CanAdminEdit() bool {
	if p == nil || p.Viewer == nil {
		return false
	}
	return hackathonViewerFromIdentity(p.Viewer, p.Conf).Admin
}

func (p *HackathonPage) AdminEditURL() string {
	if p == nil || p.Competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(p.Competition.ID)
}

func (p *HackathonPage) JudgingURL() string {
	if p == nil || p.Competition == nil {
		return "/hackathons"
	}
	return hackathonURL(p.Competition) + "/judging"
}

func (p *HackathonPage) CompetitionStatusLabel() string {
	if p == nil || p.Competition == nil {
		return ""
	}
	return hackathonLifecycleLabel(p.Competition)
}

func (p *HackathonPage) ProjectStatusLabel(status string) string {
	return hackathonStatusLabel(status)
}

func (p *HackathonPage) ProjectNumberLabel(project *types.HackathonProject) string {
	if project == nil || project.ProjectNumber == nil {
		return "Unassigned"
	}
	return fmt.Sprintf("#%d", *project.ProjectNumber)
}

func (p *HackathonPage) JudgeTypeLabel(judgeType string) string {
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

func (p *HackathonPage) NextMilestoneLabel() string {
	if p == nil {
		return ""
	}
	label, _ := hackathonNextMilestone(p.Competition)
	return label
}

func (p *HackathonPage) NextMilestoneValue() string {
	if p == nil {
		return ""
	}
	_, value := hackathonNextMilestone(p.Competition)
	return value
}

func (p *HackathonPage) NextMilestoneIsScheduleLink() bool {
	if p == nil {
		return false
	}
	return hackathonMilestoneIsScheduleLink(p.Competition)
}

func (p *HackathonPage) ScheduleURL() string {
	if p == nil {
		return hackathonScheduleURL(nil)
	}
	return hackathonScheduleURL(p.Competition)
}

func (p *HackathonPage) ScheduleEvents() []HackathonScheduleEvent {
	if p == nil || p.Competition == nil {
		return nil
	}
	competition := p.Competition
	publicAt := competition.PublicGalleryAt
	if publicAt == nil {
		publicAt = competition.SubmissionsCloseAt
	}
	return []HackathonScheduleEvent{
		{Label: "Submissions open", Time: competition.SubmissionsOpenAt},
		{Label: "Submissions close", Time: competition.SubmissionsCloseAt},
		{Label: "Submissions go public", Time: publicAt},
	}
}

func (p *HackathonPage) TimezoneOptions() []string {
	options := []string{}
	seen := map[string]bool{}
	add := func(tz string) {
		tz = strings.TrimSpace(tz)
		if tz == "" || seen[tz] {
			return
		}
		seen[tz] = true
		options = append(options, tz)
	}
	if p != nil && p.Conf != nil {
		add(p.Conf.Timezone)
	}
	add("UTC")
	add("America/New_York")
	add("America/Chicago")
	add("America/Denver")
	add("America/Los_Angeles")
	add("Europe/London")
	add("Europe/Berlin")
	add("Asia/Tokyo")
	return options
}

func hackathonNextMilestone(competition *types.HackathonCompetition) (string, string) {
	if competition == nil {
		return "", ""
	}
	now := time.Now()
	if competition.SubmissionsOpenAt != nil && competition.SubmissionsOpenAt.After(now) {
		return "Submissions open", formatHackathonTime(competition.SubmissionsOpenAt)
	}
	if competition.SubmissionsCloseAt != nil && competition.SubmissionsCloseAt.After(now) {
		return "Submissions close", formatHackathonTime(competition.SubmissionsCloseAt)
	}
	if competition.PublicGalleryAt != nil && competition.PublicGalleryAt.After(now) {
		return "Submissions go public", formatHackathonTime(competition.PublicGalleryAt)
	}
	if hackathonMilestoneIsScheduleLink(competition) {
		return "View schedule", completedHackathonScheduleValue(competition)
	}
	return "", ""
}

func hackathonMilestoneIsScheduleLink(competition *types.HackathonCompetition) bool {
	if competition == nil {
		return false
	}
	now := time.Now()
	if competition.SubmissionsOpenAt != nil && competition.SubmissionsOpenAt.After(now) {
		return false
	}
	if competition.SubmissionsCloseAt != nil && competition.SubmissionsCloseAt.After(now) {
		return false
	}
	if competition.PublicGalleryAt != nil && competition.PublicGalleryAt.After(now) {
		return false
	}
	return competition.SubmissionsOpenAt != nil || competition.SubmissionsCloseAt != nil || competition.PublicGalleryAt != nil
}

func hackathonScheduleURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/hackathons"
	}
	return hackathonURL(competition) + "/schedule"
}

func formatHackathonTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func completedHackathonScheduleValue(competition *types.HackathonCompetition) string {
	if competition == nil {
		return ""
	}
	if competition.SubmissionsOpenAt != nil && competition.SubmissionsCloseAt != nil {
		return formatHackathonTime(competition.SubmissionsOpenAt) + " - " + formatHackathonTime(competition.SubmissionsCloseAt)
	}
	if competition.PublicGalleryAt != nil {
		return formatHackathonTime(competition.PublicGalleryAt)
	}
	if competition.SubmissionsCloseAt != nil {
		return formatHackathonTime(competition.SubmissionsCloseAt)
	}
	if competition.SubmissionsOpenAt != nil {
		return formatHackathonTime(competition.SubmissionsOpenAt)
	}
	return ""
}

func HackathonShow(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, projects, err := loadHackathonPageData(w, r, ctx)
	if err != nil {
		return
	}
	personID := hackathonViewerPersonID(id)
	viewer := hackathonViewerFromIdentity(id, conf)
	page := &HackathonPage{
		Competition:   competition,
		Conf:          conf,
		Projects:      projects,
		Viewer:        id,
		OwnedProjects: ownedProjectMap(ctx, projects, personID),
		CanCreate:     id != nil && competitionAcceptsProjects(competition),
		CanJudge:      viewer.Admin || viewer.Coordinator || viewerCanJudgeCompetition(ctx, competition.ID, personID),
		FlashMessage:  r.URL.Query().Get("flash"),
		FlashError:    r.URL.Query().Get("error"),
		Year:          helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s template: %s", competition.Slug, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonSchedule(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	page := &HackathonPage{
		Competition: competition,
		Conf:        conf,
		Viewer:      id,
		Year:        helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_schedule.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/schedule template: %s", competition.Slug, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonJudging(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, events, err := loadHackathonJudgingAccess(w, r, ctx)
	if err != nil {
		return
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, viewer)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/judging list projects: %s", competition.Slug, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return
	}
	page := &HackathonPage{
		Competition:  competition,
		Conf:         conf,
		Projects:     projects,
		JudgeEvents:  events,
		Viewer:       id,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_judging.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/judging template: %s", competition.Slug, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonProjectNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	if id == nil {
		redirectHackathonLogin(w, r)
		return
	}
	if !competitionAcceptsProjects(competition) {
		http.Redirect(w, r, hackathonURL(competition)+"?error="+url.QueryEscape("Project submissions are not open."), http.StatusSeeOther)
		return
	}
	page := &HackathonPage{
		Competition:  competition,
		Conf:         conf,
		Project:      &types.HackathonProject{CompetitionID: competition.ID},
		Viewer:       id,
		IsNew:        true,
		CanEdit:      true,
		CanSubmit:    false,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_project.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/new template: %s", competition.Slug, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonProjectCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, _, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	if id == nil {
		redirectHackathonLogin(w, r)
		return
	}
	if !competitionAcceptsProjects(competition) {
		http.Redirect(w, r, hackathonURL(competition)+"?error="+url.QueryEscape("Project submissions are not open."), http.StatusSeeOther)
		return
	}
	personID := hackathonViewerPersonID(id)
	if personID == "" {
		http.Redirect(w, r, hackathonURL(competition)+"?error="+url.QueryEscape("Your account needs a person profile before you can create a project."), http.StatusSeeOther)
		return
	}
	in, err := projectInputFromRequest(w, r, competition.ID)
	if err != nil {
		http.Redirect(w, r, hackathonURL(competition)+"/projects/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	in.CreatedByPersonID = personID
	projectID, err := getters.CreateProject(ctx, in)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s create project: %s", competition.Slug, err)
		http.Redirect(w, r, hackathonURL(competition)+"/projects/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, hackathonURL(competition)+"/projects/"+url.PathEscape(projectID)+"?flash="+url.QueryEscape("Project created"), http.StatusSeeOther)
}

func HackathonProjectEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	canEdit := id != nil && projectEditableByDeadline(competition)
	canSubmit := canEdit && project.Status != getters.ProjectStatusSubmitted
	members, err := getters.ListProjectMembers(ctx, project.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s members: %s", competition.Slug, project.ID, err)
		http.Error(w, "Unable to load project members", http.StatusInternalServerError)
		return
	}
	page := &HackathonPage{
		Competition:  competition,
		Conf:         conf,
		Project:      project,
		Members:      members,
		Viewer:       id,
		CanEdit:      canEdit,
		CanSubmit:    canSubmit,
		InviteLink:   strings.TrimSpace(r.URL.Query().Get("invite")),
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_project.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s template: %s", competition.Slug, project.ID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonProjectUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, _, _, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	dest := hackathonURL(competition) + "/projects/" + url.PathEscape(project.ID)
	if !projectEditableByDeadline(competition) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project edits are closed."), http.StatusSeeOther)
		return
	}
	in, err := projectInputFromRequest(w, r, competition.ID)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateProject(ctx, project.ID, in); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s update: %s", competition.Slug, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project saved"), http.StatusSeeOther)
}

func HackathonProjectSubmit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, _, _, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	dest := hackathonURL(competition) + "/projects/" + url.PathEscape(project.ID)
	if !projectEditableByDeadline(competition) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project submissions are closed."), http.StatusSeeOther)
		return
	}
	if err := getters.SubmitProject(ctx, project.ID); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s submit: %s", competition.Slug, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project submitted"), http.StatusSeeOther)
}

func HackathonProjectInviteCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, _, _, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	dest := hackathonURL(competition) + "/projects/" + url.PathEscape(project.ID)
	if !projectEditableByDeadline(competition) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project edits are closed."), http.StatusSeeOther)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	token, _, err := getters.CreateProjectInvite(ctx, project.ID, r.FormValue("Email"), nil)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s invite: %s", competition.Slug, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	inviteURL := absoluteURL(r, "/hackathons/invites/"+url.PathEscape(token))
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Invite link created")+"&invite="+url.QueryEscape(inviteURL), http.StatusSeeOther)
}

func HackathonProjectInviteAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
	if email == "" {
		redirectHackathonLogin(w, r)
		return
	}
	personID, err := getters.GetPersonIDByEmail(ctx, email)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites person %s: %s", email, err)
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("Your account needs a person profile before you can join a project."), http.StatusSeeOther)
		return
	}
	invite, err := getters.AcceptProjectInvite(ctx, mux.Vars(r)["token"], personID)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites accept: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	project, err := getters.GetProjectByID(ctx, invite.ProjectID)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites project %s: %s", invite.ProjectID, err)
		http.Error(w, "Unable to load project", http.StatusInternalServerError)
		return
	}
	competition, err := getters.GetCompetitionByID(ctx, project.CompetitionID)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites competition %s: %s", project.CompetitionID, err)
		http.Error(w, "Unable to load hackathon", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, hackathonURL(competition)+"/projects/"+url.PathEscape(project.ID)+"?flash="+url.QueryEscape("Joined project"), http.StatusSeeOther)
}

func loadHackathonPageData(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, []*types.HackathonProject, error) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, viewer)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s list projects: %s", competition.Slug, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return nil, nil, nil, nil, err
	}
	return competition, conf, id, projects, nil
}

func loadHackathonCompetition(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, error) {
	slug := mux.Vars(r)["slug"]
	competition, err := getters.GetCompetitionBySlug(ctx, slug)
	if err != nil {
		handle404(w, r, ctx)
		return nil, nil, nil, err
	}
	var conf *types.Conf
	if competition.ConferenceID != "" {
		conf, err = getters.GetConfByRef(ctx, competition.ConferenceID)
		if err != nil {
			ctx.Err.Printf("/hackathons/%s conf %s: %s", competition.Slug, competition.ConferenceID, err)
		}
	}
	id := auth.RequireOptional(r, ctx)
	viewer := hackathonViewerFromIdentity(id, conf)
	if competition.Visibility != getters.CompetitionVisibilityPublic && !viewer.Admin {
		handle404(w, r, ctx)
		return nil, nil, nil, fmt.Errorf("hidden competition %s", competition.Slug)
	}
	return competition, conf, id, nil
}

func loadHackathonJudgingAccess(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, []*types.JudgeEvent, error) {
	slug := mux.Vars(r)["slug"]
	competition, err := getters.GetCompetitionBySlug(ctx, slug)
	if err != nil {
		handle404(w, r, ctx)
		return nil, nil, nil, nil, err
	}
	var conf *types.Conf
	if competition.ConferenceID != "" {
		conf, err = getters.GetConfByRef(ctx, competition.ConferenceID)
		if err != nil {
			ctx.Err.Printf("/hackathons/%s conf %s: %s", competition.Slug, competition.ConferenceID, err)
		}
	}
	id := auth.RequireOptional(r, ctx)
	if id == nil {
		redirectHackathonLogin(w, r)
		return nil, nil, nil, nil, fmt.Errorf("not logged in")
	}
	events, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/judging events: %s", competition.Slug, err)
		http.Error(w, "Unable to load judge events", http.StatusInternalServerError)
		return nil, nil, nil, nil, err
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	if !viewer.Admin && !viewer.Coordinator && !viewerCanJudgeCompetition(ctx, competition.ID, viewer.PersonID) {
		handle404(w, r, ctx)
		return nil, nil, nil, nil, fmt.Errorf("viewer cannot judge competition %s", competition.ID)
	}
	return competition, conf, id, events, nil
}

func loadEditableHackathonProject(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, *types.HackathonProject, error) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if id == nil {
		redirectHackathonLogin(w, r)
		return nil, nil, nil, nil, fmt.Errorf("not logged in")
	}
	projectID := mux.Vars(r)["projectID"]
	project, err := getters.GetProjectByID(ctx, projectID)
	if err != nil || project.CompetitionID != competition.ID {
		handle404(w, r, ctx)
		if err == nil {
			err = fmt.Errorf("project %s is not in competition %s", projectID, competition.ID)
		}
		return nil, nil, nil, nil, err
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	if !viewer.Admin && !viewer.Coordinator && !viewerCanManageProject(ctx, project.ID, viewer.PersonID) {
		http.Redirect(w, r, hackathonURL(competition)+"?error="+url.QueryEscape("Only project members can edit that project."), http.StatusSeeOther)
		return nil, nil, nil, nil, fmt.Errorf("viewer cannot edit project %s", project.ID)
	}
	return competition, conf, id, project, nil
}

func projectInputFromRequest(w http.ResponseWriter, r *http.Request, competitionID string) (getters.ProjectInput, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return getters.ProjectInput{}, fmt.Errorf("bad form")
	}
	in := getters.ProjectInput{
		CompetitionID:    competitionID,
		Slug:             strings.TrimSpace(r.FormValue("Slug")),
		Title:            strings.TrimSpace(r.FormValue("Title")),
		ShortDescription: strings.TrimSpace(r.FormValue("ShortDescription")),
		Description:      strings.TrimSpace(r.FormValue("Description")),
		GitHubURL:        strings.TrimSpace(r.FormValue("GitHubURL")),
		DemoURL:          strings.TrimSpace(r.FormValue("DemoURL")),
		VideoURL:         strings.TrimSpace(r.FormValue("VideoURL")),
		SlidesURL:        strings.TrimSpace(r.FormValue("SlidesURL")),
		DocsURL:          strings.TrimSpace(r.FormValue("DocsURL")),
		Tags:             splitProjectTags(r.FormValue("Tags")),
	}
	if in.Title == "" {
		return getters.ProjectInput{}, fmt.Errorf("project title is required")
	}
	if in.Slug == "" {
		return getters.ProjectInput{}, fmt.Errorf("project slug is required")
	}
	return in, nil
}

func splitProjectTags(raw string) []string {
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func hackathonViewerFromIdentity(id *auth.Identity, conf *types.Conf) types.HackathonViewer {
	if id == nil {
		return types.HackathonViewer{}
	}
	viewer := types.HackathonViewer{
		PersonID: hackathonViewerPersonID(id),
		Admin:    id.IsGlobalAdmin(),
	}
	if conf != nil {
		viewer.Admin = viewer.Admin || id.HasRoleForConf(conf.Tag, auth.RoleAdmin)
		viewer.Coordinator = id.HasRoleForConf(conf.Tag, auth.RoleVolcoord)
	}
	return viewer
}

func hackathonViewerPersonID(id *auth.Identity) string {
	if id == nil || id.Speaker == nil {
		return ""
	}
	return id.Speaker.ID
}

func competitionAcceptsProjects(competition *types.HackathonCompetition) bool {
	if competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic {
		return false
	}
	if competition.SubmissionsOpenAt == nil {
		return false
	}
	now := time.Now()
	if competition.SubmissionsOpenAt.After(now) {
		return false
	}
	return projectEditableByDeadline(competition)
}

func projectEditableByDeadline(competition *types.HackathonCompetition) bool {
	if competition == nil {
		return false
	}
	return competition.SubmissionsCloseAt == nil || competition.SubmissionsCloseAt.After(time.Now())
}

func viewerCanManageProject(ctx *config.AppContext, projectID, personID string) bool {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return false
	}
	members, err := getters.ListProjectMembers(ctx, projectID)
	if err != nil {
		ctx.Err.Printf("list project members %s: %s", projectID, err)
		return false
	}
	for _, member := range members {
		if member != nil && member.PersonID == personID {
			return true
		}
	}
	return false
}

func viewerCanJudgeCompetition(ctx *config.AppContext, competitionID, personID string) bool {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return false
	}
	judges, err := getters.ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("list competition judges %s: %s", competitionID, err)
		return false
	}
	for _, judge := range judges {
		if judge != nil && judge.PersonID == personID {
			return true
		}
	}
	return false
}

func ownedProjectMap(ctx *config.AppContext, projects []*types.HackathonProject, personID string) map[string]bool {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, project := range projects {
		if project != nil && viewerCanManageProject(ctx, project.ID, personID) {
			out[project.ID] = true
		}
	}
	return out
}

func absoluteURL(r *http.Request, path string) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	return scheme + "://" + r.Host + path
}

func redirectHackathonLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
}

func hackathonURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/hackathons"
	}
	return "/hackathons/" + url.PathEscape(competition.Slug)
}

func hackathonLifecycleLabel(competition *types.HackathonCompetition) string {
	if competition == nil {
		return ""
	}
	now := time.Now()
	if competition.SubmissionsOpenAt == nil {
		return "Schedule TBA"
	}
	if competition.SubmissionsOpenAt.After(now) {
		return "Upcoming"
	}
	if competition.SubmissionsCloseAt == nil || competition.SubmissionsCloseAt.After(now) {
		return "Open"
	}
	publicAt := competition.PublicGalleryAt
	if publicAt == nil {
		publicAt = competition.SubmissionsCloseAt
	}
	if publicAt != nil && publicAt.After(now) {
		return "Submissions closed"
	}
	return "Submissions public"
}

func hackathonStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "created":
		return "Created"
	case "submitted":
		return "Submitted"
	case "withdrawn":
		return "Withdrawn"
	case "noshow":
		return "No-show"
	case "finalist":
		return "Finalist"
	case "disqualified":
		return "Disqualified"
	case "shipped":
		return "Shipped"
	default:
		return strings.TrimSpace(status)
	}
}
