package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func registerConferenceRoutes(r *mux.Router, app *config.AppContext) {
	// Public conf routes — canonical short form `/{tag}`. Registered
	// last among single-segment routes so the literal entries above
	// (/dashboard, /login, /talk, /sponsor, /privacy, the theme
	// aliases, ...) win first. Unknown {conf} falls through to a
	// clean 404 via the handlers' FindConf branch.
	r.HandleFunc("/{conf}/agenda", func(w http.ResponseWriter, r *http.Request) {
		RenderConfAgenda(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/speakers", func(w http.ResponseWriter, r *http.Request) {
		RenderConfSpeakers(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}", func(w http.ResponseWriter, r *http.Request) {
		RenderConf(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/satellites/new", func(w http.ResponseWriter, r *http.Request) {
		SatelliteEventSuggest(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/satellites/new", func(w http.ResponseWriter, r *http.Request) {
		SatelliteEventSuggestSubmit(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/satellites/upload-img", func(w http.ResponseWriter, r *http.Request) {
		SatelliteEventSuggestImageUpload(w, r, requestApp(r, app))
	}).Methods("POST")
	r.HandleFunc("/{conf}/talks", func(w http.ResponseWriter, r *http.Request) {
		params := mux.Vars(r)
		redirectToConfAgenda(w, r, params["conf"])
	}).Methods("GET")
	r.HandleFunc("/{conf}/talk/{anchor}/calendar.ics", func(w http.ResponseWriter, r *http.Request) {
		TalkPublicICS(w, r, requestApp(r, app))
	}).Methods("GET")
	r.HandleFunc("/{conf}/success", func(w http.ResponseWriter, r *http.Request) {
		RenderConfSuccess(w, r, requestApp(r, app))
	}).Methods("GET")
}
