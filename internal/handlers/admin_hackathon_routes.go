package handlers

import (
	"net/http"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"

	"github.com/gorilla/mux"
)

type hackathonAdminHandler func(http.ResponseWriter, *http.Request, *config.AppContext)

func registerConferenceHackathonAdminRoutes(r *mux.Router, app *config.AppContext) {
	register := func(path string, handler hackathonAdminHandler, methods ...string) {
		r.HandleFunc("/{conf}/admin/hackathon"+path, conferenceHackathonAdminHandler(app, handler)).Methods(methods...)
	}
	register("", HackathonAdminEdit, http.MethodGet)
	register("", HackathonAdminUpdate, http.MethodPost)
	register("/projects", HackathonAdminProjects, http.MethodGet)
	register("/projects", HackathonAdminCreateProject, http.MethodPost)
	register("/projects/assign-numbers", HackathonAdminAssignProjectNumbers, http.MethodPost)
	register("/projects/{projectID}", HackathonAdminUpdateProject, http.MethodPost)
	register("/projects/{projectID}/delete", HackathonAdminDeleteProject, http.MethodPost)
	register("/timeline", HackathonAdminTimeline, http.MethodGet)
	register("/timeline", HackathonAdminUpdateTimeline, http.MethodPost)
	register("/people/search", HackathonAdminPersonSearch, http.MethodGet)
	register("/judging", HackathonAdminJudging, http.MethodGet)
	register("/judging/scores", HackathonAdminScoreReview, http.MethodGet)
	register("/judging/advance", HackathonAdminAdvanceProjects, http.MethodPost)
	register("/judging/scores/remove-ballot", HackathonAdminRemoveJudgeBallot, http.MethodPost)
	register("/judging/events", HackathonAdminCreateJudgeEvent, http.MethodPost)
	register("/judging/events/ranks", HackathonAdminUpdateJudgeEventRanks, http.MethodPost)
	register("/judging/events/state", HackathonAdminUpdateJudgeEventState, http.MethodPost)
	register("/judging/events/delete", HackathonAdminDeleteJudgeEvent, http.MethodPost)
	register("/judging/judges", HackathonAdminAddJudge, http.MethodPost)
	register("/judging/judges/roles", HackathonAdminUpdateJudgeRoles, http.MethodPost)
	register("/judging/judges/order", HackathonAdminUpdateJudgeOrder, http.MethodPost)
	register("/judging/judges/invites", HackathonAdminCreateJudgeInvite, http.MethodPost)
	register("/judging/judges/remove", HackathonAdminRemoveJudge, http.MethodPost)
	register("/awards", HackathonAdminAwards, http.MethodGet)
	register("/awards", HackathonAdminCreateAward, http.MethodPost)
	register("/awards/update", HackathonAdminUpdateAward, http.MethodPost)
	register("/awards/archive", HackathonAdminArchiveAward, http.MethodPost)
	register("/awards/restore", HackathonAdminRestoreAward, http.MethodPost)
	register("/awards/delete", HackathonAdminDeleteArchivedAward, http.MethodPost)
	register("/awards/prizes", HackathonAdminCreatePrize, http.MethodPost)
	register("/awards/prizes/update", HackathonAdminUpdatePrize, http.MethodPost)
	register("/awards/prizes/delete", HackathonAdminDeletePrize, http.MethodPost)
	register("/awards/assign", HackathonAdminAssignAward, http.MethodPost)
	register("/awards/remove", HackathonAdminRemoveAward, http.MethodPost)
	register("/payouts", HackathonAdminPayouts, http.MethodGet)
	register("/payouts", HackathonAdminCreateDistribution, http.MethodPost)
	register("/payouts/prepare", HackathonAdminPrepareDistributions, http.MethodPost)
	register("/payouts/{distributionID}", HackathonAdminUpdateDistribution, http.MethodPost)
	register("/payouts/tax/{personID}", HackathonAdminDownloadTaxForm, http.MethodGet)
	register("/results/finalize", HackathonAdminFinalizeResults, http.MethodPost)
	register("/results/reopen", HackathonAdminReopenResults, http.MethodPost)
	register("/visibility", HackathonAdminUpdateVisibility, http.MethodPost)
}

func conferenceHackathonAdminHandler(app *config.AppContext, next hackathonAdminHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		confTag := strings.TrimSpace(mux.Vars(r)["conf"])
		conf, err := getters.GetConfByTag(app, confTag)
		if err != nil || conf == nil {
			handle404(w, r, app)
			return
		}
		competition, err := getters.GetCompetitionByConferenceID(app, conf.Ref)
		if err != nil || competition == nil {
			handle404(w, r, app)
			return
		}
		vars := mux.Vars(r)
		vars["competitionID"] = competition.ID
		next(w, mux.SetURLVars(r, vars), app)
	}
}
