package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func registerConferenceAdminRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/{conf}/admin/gifts", func(w http.ResponseWriter, r *http.Request) {
		AdminGifts(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/gifts/clipart.zip", func(w http.ResponseWriter, r *http.Request) {
		AdminGiftsClipartZip(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/comp-tickets", func(w http.ResponseWriter, r *http.Request) {
		AdminCompTickets(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/{conf}/admin/discounts", func(w http.ResponseWriter, r *http.Request) {
		AdminDiscounts(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/{conf}/admin/recordings", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminList(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/recordings/oauth/youtube/start", func(w http.ResponseWriter, r *http.Request) {
		RecordingsYTOAuthStart(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/recordings/oauth/youtube/callback", func(w http.ResponseWriter, r *http.Request) {
		RecordingsYTOAuthCallback(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/recordings/oauth/youtube/callback", func(w http.ResponseWriter, r *http.Request) {
		RecordingsYTOAuthCallback(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/recordings/oauth/youtube/disconnect", func(w http.ResponseWriter, r *http.Request) {
		RecordingsYTOAuthDisconnect(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/x/auth-check", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminXAuthCheck(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/autoschedule", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminAutoschedulePreview(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/recordings/autoschedule", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminAutoscheduleApply(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/upload-youtube", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminBulkUploadYTPreview(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/recordings/upload-youtube", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminBulkUploadYTApply(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/youtube-slots", func(w http.ResponseWriter, r *http.Request) {
		RecordingsYouTubeSlots(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminDetail(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/recordings/{id}/upload-yt", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminUploadYT(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}/schedule", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminSchedule(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminUploadSourceFile(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}/x-copy", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminSaveXCopy(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}/post-x", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminPostXNow(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}/schedule-x", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminScheduleX(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}/x", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminSaveXLink(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}/retry-x", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminRetryX(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/recordings/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminJobStatus(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/recordings/{id}/x-status", func(w http.ResponseWriter, r *http.Request) {
		RecordingsAdminXJobStatus(w, r, requestApp(r, app))
	}).Methods("GET")

	// Dev-only smoke endpoint for the self-hosted ICS pipeline.
	// Production registrations of the route are blocked at the
	// handler boundary (TrialCalInvite checks ctx.Env.Prod).
	r.HandleFunc("/trial-cal-invite", func(w http.ResponseWriter, r *http.Request) {
		TrialCalInvite(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/admin/orgs", func(w http.ResponseWriter, r *http.Request) {
		OrgList(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/admin/orgs/new", func(w http.ResponseWriter, r *http.Request) {
		OrgNew(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/orgs/new", func(w http.ResponseWriter, r *http.Request) {
		OrgCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/admin/orgs/upload-logo", func(w http.ResponseWriter, r *http.Request) {
		OrgLogoUpload(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/admin/orgs/{ref}", func(w http.ResponseWriter, r *http.Request) {
		OrgDetail(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/admin/orgs/{ref}", func(w http.ResponseWriter, r *http.Request) {
		OrgSave(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/sponsors", func(w http.ResponseWriter, r *http.Request) {
		SponsorshipsList(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/admin/sponsors/new", func(w http.ResponseWriter, r *http.Request) {
		SponsorshipCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/sponsors/{ref}", func(w http.ResponseWriter, r *http.Request) {
		SponsorshipUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/sponsors/{ref}/delete", func(w http.ResponseWriter, r *http.Request) {
		SponsorshipDelete(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/social", func(w http.ResponseWriter, r *http.Request) {
		SocialAdmin(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/admin/social/post", func(w http.ResponseWriter, r *http.Request) {
		SocialPost(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/speakers", func(w http.ResponseWriter, r *http.Request) {
		SpeakerAdmin(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/admin/speakers/featured", func(w http.ResponseWriter, r *http.Request) {
		SpeakerAdminFeatured(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/speakers/new", func(w http.ResponseWriter, r *http.Request) {
		SpeakerAdminNew(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	r.HandleFunc("/{conf}/admin/speakers/{speakerID}/refresh-cards", func(w http.ResponseWriter, r *http.Request) {
		AdminSpeakerRefreshCards(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/speakers/{speakerID}/edit", func(w http.ResponseWriter, r *http.Request) {
		SpeakerAdminEdit(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	r.HandleFunc("/{conf}/admin/speakerconfs/{speakerConfID}/edit", func(w http.ResponseWriter, r *http.Request) {
		SpeakerConfAdminEdit(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	r.HandleFunc("/{conf}/admin/speakers/email", func(w http.ResponseWriter, r *http.Request) {
		SpeakerAdminBulkEmail(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/registrations", func(w http.ResponseWriter, r *http.Request) {
		RegistrationsAdmin(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/admin/registrations/email", func(w http.ResponseWriter, r *http.Request) {
		RegistrationsAdminBulkEmail(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/registrations/check-in", func(w http.ResponseWriter, r *http.Request) {
		RegistrationsAdminBulkCheckIn(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/registrations/merch/{itemID}/pickup", func(w http.ResponseWriter, r *http.Request) {
		RegistrationsAdminMerchPickup(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/applicants", func(w http.ResponseWriter, r *http.Request) {
		ProposalAdmin(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/admin", func(w http.ResponseWriter, r *http.Request) {
		OrganizerDashboard(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/", func(w http.ResponseWriter, r *http.Request) {
		OrganizerDashboard(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/details", func(w http.ResponseWriter, r *http.Request) {
		GlobalAdminEventDetails(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/details", func(w http.ResponseWriter, r *http.Request) {
		GlobalAdminUpdateConfDetails(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/details/confinfo", func(w http.ResponseWriter, r *http.Request) {
		GlobalAdminUpdateConfInfo(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/details/ticket", func(w http.ResponseWriter, r *http.Request) {
		GlobalAdminUpdateConfTicket(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/details/merch-upsells", func(w http.ResponseWriter, r *http.Request) {
		GlobalAdminUpdateConfMerchUpsells(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/state", func(w http.ResponseWriter, r *http.Request) {
		GlobalAdminUpdateConfState(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/review", func(w http.ResponseWriter, r *http.Request) {
		ReviewProposals(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/review/{proposalID}/{action}", func(w http.ResponseWriter, r *http.Request) {
		ReviewProposalAction(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/proposal/{proposalID}/invite", func(w http.ResponseWriter, r *http.Request) {
		AdminProposalInviteLink(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/proposal/{proposalID}/edit", func(w http.ResponseWriter, r *http.Request) {
		AdminEditProposal(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/{conf}/admin/proposal/{proposalID}/speakers/attach", func(w http.ResponseWriter, r *http.Request) {
		AdminEditProposalAttachSpeaker(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/proposal/{proposalID}/speakers/{speakerConfID}/remove", func(w http.ResponseWriter, r *http.Request) {
		AdminProposalRemoveSpeaker(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/invite-speaker", func(w http.ResponseWriter, r *http.Request) {
		AdminInviteSpeaker(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/invite-speaker", func(w http.ResponseWriter, r *http.Request) {
		AdminInviteSpeakerSubmit(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/invite-speaker/sent", func(w http.ResponseWriter, r *http.Request) {
		AdminInviteSpeakerSent(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/admin/hotels", func(w http.ResponseWriter, r *http.Request) {
		HotelsAdmin(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/hotels", func(w http.ResponseWriter, r *http.Request) {
		HotelsAdminSave(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/hotels/upload-img", func(w http.ResponseWriter, r *http.Request) {
		HotelImageUpload(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/satellites", func(w http.ResponseWriter, r *http.Request) {
		SatelliteEventsAdmin(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/satellites", func(w http.ResponseWriter, r *http.Request) {
		SatelliteEventsAdminSave(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/satellites/upload-img", func(w http.ResponseWriter, r *http.Request) {
		SatelliteEventsAdminImageUpload(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/schedule", func(w http.ResponseWriter, r *http.Request) {
		ScheduleConf(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/cliparts", func(w http.ResponseWriter, r *http.Request) {
		AdminCliparts(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/cliparts/{proposalID}", func(w http.ResponseWriter, r *http.Request) {
		AdminClipartsUpload(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/social-cards.zip", func(w http.ResponseWriter, r *http.Request) {
		AdminSocialCardsZip(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/{conf}/admin/run-of-show", func(w http.ResponseWriter, r *http.Request) {
		RunOfShowAdmin(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/run-of-show/adjust", func(w http.ResponseWriter, r *http.Request) {
		RunOfShowAdjust(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/run-of-show", func(w http.ResponseWriter, r *http.Request) {
		RunOfShowPublic(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/run-of-show/events", func(w http.ResponseWriter, r *http.Request) {
		RunOfShowEvents(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/admin/schedule/sendcal-updates", func(w http.ResponseWriter, r *http.Request) {
		ScheduleSendCalUpdates(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/schedule/place", func(w http.ResponseWriter, r *http.Request) {
		SchedulePlace(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/schedule/unplace", func(w http.ResponseWriter, r *http.Request) {
		ScheduleUnplace(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/schedule/resize", func(w http.ResponseWriter, r *http.Request) {
		ScheduleResize(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/schedule/add-hackathon", func(w http.ResponseWriter, r *http.Request) {
		ScheduleAddHackathon(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/schedule/add-talk", func(w http.ResponseWriter, r *http.Request) {
		ScheduleAddTalk(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/applicants/email", func(w http.ResponseWriter, r *http.Request) {
		ProposalAdminBulkEmail(w, r, requestApp(r, app))
	}).Methods("POST")

	r.HandleFunc("/{conf}/admin/applicants/accept", func(w http.ResponseWriter, r *http.Request) {
		ProposalAdminAccept(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/applicants/resend-tickets", func(w http.ResponseWriter, r *http.Request) {
		AdminResendSpeakerTickets(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/applicants/{proposalID}/cancel", func(w http.ResponseWriter, r *http.Request) {
		AdminCancelTalk(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/applicants/{proposalID}/refresh-card", func(w http.ResponseWriter, r *http.Request) {
		AdminProposalRefreshCard(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/proposals/{proposalID}/sendcal", func(w http.ResponseWriter, r *http.Request) {
		AdminProposalSendCal(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/applicants/sendcal-all", func(w http.ResponseWriter, r *http.Request) {
		AdminProposalSendCalAll(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/admin/speakers/sendcal", func(w http.ResponseWriter, r *http.Request) {
		if id := requireConfAdmin(w, r, requestApp(r, app)); id == nil {
			return
		}
		SendCals(w, r, requestApp(r, app))

		params := mux.Vars(r)
		confTag := params["conf"]
		http.Redirect(w, r, "/"+confTag+"/admin/speakers?flash=Calendar+invites+sent", http.StatusFound)
	}).Methods("GET", "POST")

	// 301-redirect every legacy admin URL (/admin/{tag}/...,
	// /vols/admin/{tag}/..., etc.) to the new /{conf}/{role}/...
	// shape. Registered last so it doesn't shadow live routes.
	RegisterAdminRedirects(r)
}
