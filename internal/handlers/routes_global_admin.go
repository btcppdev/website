package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func registerGlobalAdminRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		GlobalAdminDashboard(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/roles", func(w http.ResponseWriter, r *http.Request) {
		SpeakerRolesUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/homepage-speakers", func(w http.ResponseWriter, r *http.Request) {
		GlobalAdminHomepageSpeakersUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/people", func(w http.ResponseWriter, r *http.Request) {
		AdminPeople(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/people/merge", func(w http.ResponseWriter, r *http.Request) {
		AdminPersonMerge(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/people/merge", func(w http.ResponseWriter, r *http.Request) {
		AdminPersonMergeSave(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/people/merge-requests/{requestID}/reject", func(w http.ResponseWriter, r *http.Request) {
		AdminPersonMergeRequestReject(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/people/merges/{mergeID}", func(w http.ResponseWriter, r *http.Request) {
		AdminPersonMergeAudit(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/people/merges/{mergeID}/undo", func(w http.ResponseWriter, r *http.Request) {
		AdminPersonMergeUndo(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminList(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/new", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminNew(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminEdit(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/projects", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminProjects(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/projects", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminCreateProject(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/projects/assign-numbers", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminAssignProjectNumbers(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/projects/{projectID}", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateProject(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/projects/{projectID}/delete", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminDeleteProject(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/timeline", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminTimeline(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/timeline", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateTimeline(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/people/search", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminPersonSearch(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/managers", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminManagers(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/managers", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminAddManager(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/managers/scope", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateManagerScope(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/managers/remove", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminRemoveManager(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminJudging(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/mode", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateJudgingMode(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/scores", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminScoreReview(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/advance", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminAdvanceProjects(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/scores/remove-ballot", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminRemoveJudgeBallot(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/events", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminCreateJudgeEvent(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/events/ranks", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateJudgeEventRanks(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/events/state", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateJudgeEventState(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/events/delete", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminDeleteJudgeEvent(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/judges", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminAddJudge(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/judges/roles", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateJudgeRoles(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/judges/order", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateJudgeOrder(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/judges/invites", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminCreateJudgeInvite(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/judging/judges/remove", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminRemoveJudge(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminAwards(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminCreateAward(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/update", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateAward(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/archive", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminArchiveAward(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/restore", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminRestoreAward(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/delete", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminDeleteArchivedAward(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/prizes", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminCreatePrize(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/prizes/update", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdatePrize(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/prizes/delete", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminDeletePrize(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/assign", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminAssignAward(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/remove", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminRemoveAward(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/results/finalize", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminFinalizeResults(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/results/reopen", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminReopenResults(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/judges", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminAddAwardJudge(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/awards/judges/remove", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminRemoveAwardJudge(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}/visibility", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdateVisibility(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/hackathons/{competitionID}", func(w http.ResponseWriter, r *http.Request) {
		HackathonAdminUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	registerConferenceHackathonAdminRoutes(r, app)
	r.HandleFunc("/admin/easyship", func(w http.ResponseWriter, r *http.Request) {
		AdminEasyship(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/easyship", func(w http.ResponseWriter, r *http.Request) {
		AdminEasyshipSave(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/missives", func(w http.ResponseWriter, r *http.Request) {
		TemplatedMissivesAdmin(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/missives", func(w http.ResponseWriter, r *http.Request) {
		TemplatedMissivesSave(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/missives/upload-image", func(w http.ResponseWriter, r *http.Request) {
		TemplatedMissivesUploadImage(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/missives/test-send", func(w http.ResponseWriter, r *http.Request) {
		TemplatedMissivesTestSend(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/missives/schedule", func(w http.ResponseWriter, r *http.Request) {
		TemplatedMissivesSchedule(w, r, requestApp(r, app))
	}).Methods("POST")
}
