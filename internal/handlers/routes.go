package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

func Routes(app *config.AppContext) (http.Handler, error) {
	r := mux.NewRouter()
	app.EmailCache.Initialize()

	err := loadTemplates(app)
	if err != nil {
		return r, err
	}

	err = addFaviconRoutes(r)
	if err != nil {
		return r, err
	}

	// Registration order is significant: literal routes must precede the
	// catch-all conference routes registered last.
	registerPublicRoutes(r, app)
	registerVolunteerAdminRoutes(r, app)
	registerDashboardRoutes(r, app)
	registerGlobalAdminRoutes(r, app)
	registerHackathonRoutes(r, app)
	registerShopRoutes(r, app)
	registerParticipantRoutes(r, app)
	registerConferenceAdminRoutes(r, app)
	registerConferenceRoutes(r, app)

	// Create a file server to serve static files from the "static" directory.
	// Wrap with a Cache-Control: max-age=3600 header so browsers
	// can serve cached copies without revalidating. http.FileServer
	// already emits Last-Modified, so deployments still invalidate
	// stale assets within the hour via conditional GET / 304s.
	fs := http.FileServer(http.Dir("static"))
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", staticCache(fs)))

	return requestLog(app, withRequestApp(app, withOptionalIdentity(app, redirectTrailingSlash(noIndexRobots(r))))), nil
}
