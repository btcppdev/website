package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func registerHackathonRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/{conf}/hackathon", func(w http.ResponseWriter, r *http.Request) {
		HackathonShow(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/hackathon/schedule", func(w http.ResponseWriter, r *http.Request) {
		HackathonSchedule(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/hackathon/schedule/{segmentID}/calendar.ics", func(w http.ResponseWriter, r *http.Request) {
		HackathonScheduleICS(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/hackathon/judging", func(w http.ResponseWriter, r *http.Request) {
		HackathonJudging(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/hackathon/judging/scorecards", func(w http.ResponseWriter, r *http.Request) {
		HackathonScorecardSubmit(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/hackathon/judging/award-winners", func(w http.ResponseWriter, r *http.Request) {
		HackathonAwardWinnerAssign(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/hackathon/judging/award-winners/remove", func(w http.ResponseWriter, r *http.Request) {
		HackathonAwardWinnerRemove(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/hackathon/projects/new", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectNew(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/hackathon/projects", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/hackathons/invites/{token}", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectInviteAccept(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/hackathons/judge-invites/{token}", func(w http.ResponseWriter, r *http.Request) {
		HackathonJudgeInviteAccept(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/hackathon/projects/{projectID}/invites", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectInviteCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/hackathon/projects/{projectID}/team/remove", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectMemberRemove(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/hackathon/projects/{projectID}/submit", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectSubmit(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/hackathon/projects/{projectID}/delete", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectDelete(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/hackathon/projects/{projectID}/edit", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectEdit(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/hackathon/projects/{projectID}/edit", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/hackathon/projects/{projectID}", func(w http.ResponseWriter, r *http.Request) {
		HackathonProjectShow(w, r, requestApp(r, app))
	}).Methods("GET")
}
