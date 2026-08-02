package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func registerParticipantRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/dashboard/talks/{proposalID}/edit", func(w http.ResponseWriter, r *http.Request) {
		DashboardEditProposal(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	r.HandleFunc("/dashboard/talks/{proposalID}/details", func(w http.ResponseWriter, r *http.Request) {
		DashboardTalkDetails(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/dashboard/talks/{proposalID}/resources", func(w http.ResponseWriter, r *http.Request) {
		DashboardTalkResources(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/conftalks/{confTalkID}/resources", func(w http.ResponseWriter, r *http.Request) {
		DashboardConfTalkResources(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/dashboard/talks/{proposalID}/withdraw", func(w http.ResponseWriter, r *http.Request) {
		DashboardWithdraw(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/dashboard/talks/{proposalID}/accept", func(w http.ResponseWriter, r *http.Request) {
		DashboardAcceptInvite(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/talks/{proposalID}/decline", func(w http.ResponseWriter, r *http.Request) {
		DashboardDeclineInvite(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/talks/{proposalID}/confirm", func(w http.ResponseWriter, r *http.Request) {
		DashboardConfirmTalk(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/dashboard/invite/{proposalID}", func(w http.ResponseWriter, r *http.Request) {
		DashboardInviteCoSpeaker(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/talks/{proposalID}/speakers/{speakerConfID}/remove", func(w http.ResponseWriter, r *http.Request) {
		DashboardRemoveCoSpeaker(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/speaker", func(w http.ResponseWriter, r *http.Request) {
		DashboardEditSpeaker(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/dashboard/satellites/{eventID}/edit", func(w http.ResponseWriter, r *http.Request) {
		DashboardSatelliteEventEdit(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/satellites/{eventID}/edit", func(w http.ResponseWriter, r *http.Request) {
		DashboardSatelliteEventSave(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/satellites/{eventID}/upload-img", func(w http.ResponseWriter, r *http.Request) {
		DashboardSatelliteEventImageUpload(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/invite-speaker/{proposalID}", func(w http.ResponseWriter, r *http.Request) {
		InviteSpeaker(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/invite-speaker/{proposalID}/decline", func(w http.ResponseWriter, r *http.Request) {
		InviteSpeakerDecline(w, r, requestApp(r, app))
	}).Methods("POST")

	// Backwards compat: existing magic-link emails point at /vols/shift.
	// Forward them to /dashboard, preserving the HMAC + email query params.
	r.HandleFunc("/vols/shift", func(w http.ResponseWriter, r *http.Request) {
		target := "/dashboard"
		if raw := r.URL.RawQuery; raw != "" {
			target += "?" + raw
		}
		http.Redirect(w, r, target, http.StatusFound)
	}).Methods("GET", "POST")

	r.HandleFunc("/dashboard/vol/{shiftRef}/calendar.ics", func(w http.ResponseWriter, r *http.Request) {
		DashboardVolShiftICS(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/vol/{conf}/shifts/resend-invites", func(w http.ResponseWriter, r *http.Request) {
		DashboardVolShiftsResend(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/vols/shift/{conf}", func(w http.ResponseWriter, r *http.Request) {
		VolunteerShiftSignup(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/vols/shift/{conf}/select", func(w http.ResponseWriter, r *http.Request) {
		VolunteerSelectShift(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/vols/shift/{conf}/remove", func(w http.ResponseWriter, r *http.Request) {
		VolunteerRemoveShift(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/vols/shift/{conf}/submit", func(w http.ResponseWriter, r *http.Request) {
		VolunteerSubmitShifts(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/vols/shift/{conf}/decline", func(w http.ResponseWriter, r *http.Request) {
		VolunteerDecline(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/vols/shift/{conf}/availability", func(w http.ResponseWriter, r *http.Request) {
		VolunteerUpdateAvailability(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/vols/shift/{conf}/work-prefs", func(w http.ResponseWriter, r *http.Request) {
		VolunteerUpdateWorkPrefs(w, r, requestApp(r, app))
	}).Methods("POST")
}
