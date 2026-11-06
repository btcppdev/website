package handlers

import (
	"net/http"
	"sort"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

func (p *HackathonAdminPage) JudgingSheetURL(competition *types.HackathonCompetition) string {
	return p.JudgingURL(competition) + "/print"
}

// JudgingSheetProjects keeps the paper sheet in table order, including submitted
// projects still waiting for a table number at the end.
func (p *HackathonAdminPage) JudgingSheetProjects() []*types.HackathonProject {
	projects := hackathonSubmittedProjects(p.Projects)
	sort.SliceStable(projects, func(i, j int) bool {
		a, b := projects[i], projects[j]
		if a.ProjectNumber == nil || b.ProjectNumber == nil {
			if a.ProjectNumber != b.ProjectNumber {
				return a.ProjectNumber != nil
			}
		} else if *a.ProjectNumber != *b.ProjectNumber {
			return *a.ProjectNumber < *b.ProjectNumber
		}
		if aTitle, bTitle := strings.ToLower(a.Title), strings.ToLower(b.Title); aTitle != bTitle {
			return aTitle < bTitle
		}
		return a.ID < b.ID
	})
	return projects
}

func (p *HackathonAdminPage) JudgingSheetAwards() []*types.Award {
	return availableOptInAwards(p.Awards)
}

func HackathonAdminJudgingSheet(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireHackathonAdmin(w, r, ctx)
	if id == nil {
		return
	}
	competitionID := mux.Vars(r)["competitionID"]
	competition, err := getters.GetCompetitionByID(ctx, competitionID)
	if err != nil || competition == nil {
		handle404(w, r, ctx)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		ctx.Err.Printf("hackathon %s judging sheet conference: %v", competitionID, err)
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competitionID, types.HackathonViewer{Admin: true})
	if err != nil {
		ctx.Err.Printf("hackathon %s judging sheet projects: %v", competitionID, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return
	}
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("hackathon %s judging sheet awards: %v", competitionID, err)
		http.Error(w, "Unable to load challenges", http.StatusInternalServerError)
		return
	}
	optIns, err := getters.ListProjectAwardOptInsForCompetition(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("hackathon %s judging sheet opt-ins: %v", competitionID, err)
		http.Error(w, "Unable to load challenge opt-ins", http.StatusInternalServerError)
		return
	}
	page := &HackathonAdminPage{
		Competition:          competition,
		Conf:                 conf,
		Viewer:               id,
		Projects:             projects,
		Awards:               awards,
		AwardOptInsByProject: projectAwardOptInsByProject(optIns),
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/hackathon_judging_sheet.tmpl", page); err != nil {
		ctx.Err.Printf("hackathon %s judging sheet template: %v", competitionID, err)
		http.Error(w, "Unable to load judging sheet", http.StatusInternalServerError)
	}
}
