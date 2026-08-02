package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func registerVolunteerAdminRoutes(r *mux.Router, app *config.AppContext) {
	/* Internal pages */
	r.HandleFunc("/{conf}/volcoord", func(w http.ResponseWriter, r *http.Request) {
		VolAdmin(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/volcoord/send-orientation", func(w http.ResponseWriter, r *http.Request) {
		SendVolOrientation(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/volcoord/orientation", func(w http.ResponseWriter, r *http.Request) {
		VolAdminScheduleOrientation(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/volcoord/sendcal", func(w http.ResponseWriter, r *http.Request) {
		if id := requireConfVolcoord(w, r, requestApp(r, app)); id == nil {
			return
		}
		SendVolCals(w, r, requestApp(r, app))

		params := mux.Vars(r)
		confTag := params["conf"]
		http.Redirect(w, r, "/"+confTag+"/volcoord?flash=Shift+calendar+invites+sent", http.StatusFound)
	}).Methods("GET", "POST")

	r.HandleFunc("/{conf}/volcoord/promote", func(w http.ResponseWriter, r *http.Request) {
		VolAdminPromote(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/auto-assign", func(w http.ResponseWriter, r *http.Request) {
		VolAdminAutoAssign(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/shifts", func(w http.ResponseWriter, r *http.Request) {
		VolAdminShifts(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/volcoord/shifts/new", func(w http.ResponseWriter, r *http.Request) {
		VolAdminCreateShift(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/shifts/gen", func(w http.ResponseWriter, r *http.Request) {
		VolAdminGenWorkShifts(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/shifts/{shiftRef}/reschedule", func(w http.ResponseWriter, r *http.Request) {
		VolShiftReschedule(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/shifts/{shiftRef}/update", func(w http.ResponseWriter, r *http.Request) {
		VolAdminUpdateShift(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/shifts/{shiftRef}/delete", func(w http.ResponseWriter, r *http.Request) {
		VolAdminDeleteShift(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/vol/{volRef}", func(w http.ResponseWriter, r *http.Request) {
		VolAdminDetails(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/volcoord/vol/{volRef}/status", func(w http.ResponseWriter, r *http.Request) {
		VolAdminUpdateStatus(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/vol/{volRef}/availability", func(w http.ResponseWriter, r *http.Request) {
		VolAdminUpdateAvailability(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/vol/{volRef}/work-prefs", func(w http.ResponseWriter, r *http.Request) {
		VolAdminUpdateWorkPrefs(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/vol/{volRef}/add-shift", func(w http.ResponseWriter, r *http.Request) {
		VolAdminAddShift(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/vol/{volRef}/remove-shift", func(w http.ResponseWriter, r *http.Request) {
		VolAdminRemoveShift(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/vol/{volRef}/scheduled", func(w http.ResponseWriter, r *http.Request) {
		VolAdminMarkScheduled(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/email", func(w http.ResponseWriter, r *http.Request) {
		VolAdminBulkEmail(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/volcoord/decline-selected", func(w http.ResponseWriter, r *http.Request) {
		VolAdminDeclineSelected(w, r, requestApp(r, app))
	}).Methods("POST")
}
