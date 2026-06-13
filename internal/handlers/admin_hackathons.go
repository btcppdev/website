package handlers

import (
	"fmt"
	"net/http"
	"net/url"
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
	Competitions []*types.HackathonCompetition
	Confs        []*types.Conf
	Competition  *types.HackathonCompetition
	IsNew        bool
	FlashMessage string
	FlashError   string
	Year         uint
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

func (p *HackathonAdminPage) VisibilityLabel(visibility string) string {
	return hackathonVisibilityLabel(visibility)
}

func (p *HackathonAdminPage) EditURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID)
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

func hackathonVisibilityFromForm(r *http.Request) (string, error) {
	visibility := strings.TrimSpace(r.FormValue("Visibility"))
	switch visibility {
	case getters.CompetitionVisibilityHidden, getters.CompetitionVisibilityPublic:
		return visibility, nil
	default:
		return "", fmt.Errorf("visibility must be Hidden or Public")
	}
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
