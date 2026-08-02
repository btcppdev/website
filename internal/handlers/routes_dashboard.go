package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func registerDashboardRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		Dashboard(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/dashboard/hackathons", func(w http.ResponseWriter, r *http.Request) {
		DashboardHackathons(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		Login(w, r, requestApp(r, app))
	}).Methods("GET", "POST")
	r.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		AuthLanding(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/auth/status", func(w http.ResponseWriter, r *http.Request) {
		AuthStatus(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		LogoutHandler(w, r, requestApp(r, app))
	}).Methods("POST")

	// /dashboard/affiliate/* MUST be registered before
	// /dashboard/{confTag}/edit below, otherwise mux's first-match
	// rule has /dashboard/affiliate/edit eaten by the speakerconf
	// route (confTag="affiliate" → DashboardEditSpeakerConf can't
	// find the conf, silently bounces the visitor back to
	// /dashboard with no flash).
	r.HandleFunc("/dashboard/affiliate", func(w http.ResponseWriter, r *http.Request) {
		AffiliateLanding(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/affiliate/new", func(w http.ResponseWriter, r *http.Request) {
		AffiliateNew(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/affiliate/new", func(w http.ResponseWriter, r *http.Request) {
		AffiliateCreate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/affiliate/edit", func(w http.ResponseWriter, r *http.Request) {
		AffiliateEdit(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/affiliate/edit", func(w http.ResponseWriter, r *http.Request) {
		AffiliateUpdate(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/affiliate/disable", func(w http.ResponseWriter, r *http.Request) {
		AffiliateDisable(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/hackathon-ticket-entitlements/{entitlementID}/claim", func(w http.ResponseWriter, r *http.Request) {
		DashboardClaimHackathonTicket(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/dashboard/tickets", func(w http.ResponseWriter, r *http.Request) {
		DashboardHackathonTickets(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/orders", func(w http.ResponseWriter, r *http.Request) {
		DashboardOrders(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/orders/{order}", func(w http.ResponseWriter, r *http.Request) {
		DashboardOrder(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/dashboard/orders/{order}/receipt", func(w http.ResponseWriter, r *http.Request) {
		DashboardOrderReceipt(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/dashboard/{confTag}/edit", func(w http.ResponseWriter, r *http.Request) {
		DashboardEditSpeakerConf(w, r, requestApp(r, app))
	}).Methods("GET", "POST")

	r.HandleFunc("/api/orgs/search", func(w http.ResponseWriter, r *http.Request) {
		OrgSearch(w, r, requestApp(r, app))
	}).Methods("GET")

	r.HandleFunc("/api/speakers/search", func(w http.ResponseWriter, r *http.Request) {
		SpeakerSearch(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/api/people/search", func(w http.ResponseWriter, r *http.Request) {
		PersonSearch(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/api/speakers/{speakerID}/roles", func(w http.ResponseWriter, r *http.Request) {
		SpeakerRolesGet(w, r, requestApp(r, app))
	}).Methods("GET")
}
