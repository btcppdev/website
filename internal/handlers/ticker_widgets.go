package handlers

import (
	"btcpp-web/external/getters"
	"bytes"
	"net/http"

	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

type tickerWidgetPage struct {
	SponsorsMode bool
	Sponsors     []*types.Sponsorship
	Nav          NavConfList
}

func registerTickerWidgets(router *mux.Router, app *config.AppContext) {
	router.HandleFunc("/widgets/ticker", func(w http.ResponseWriter, r *http.Request) {
		renderTickerWidget(w, r, app, tickerWidgetPage{Nav: buildNavConfList(app)})
	}).Methods(http.MethodGet)
	router.HandleFunc("/widgets/{conf}/sponsors", func(w http.ResponseWriter, r *http.Request) {
		conf, err := helpers.FindConf(r, app)
		if err != nil || conf == nil || !conf.IsPublished() {
			http.NotFound(w, r)
			return
		}
		sponsors, err := getters.ListSponsorships(app, conf.Ref)
		if err != nil {
			http.Error(w, "Sponsors unavailable", http.StatusServiceUnavailable)
			return
		}
		renderTickerWidget(w, r, app, tickerWidgetPage{SponsorsMode: true, Sponsors: sponsorBannerFromTiers(groupSponsorTiers(sponsors))})
	}).Methods(http.MethodGet)
}

func renderTickerWidget(w http.ResponseWriter, r *http.Request, app *config.AppContext, page tickerWidgetPage) {
	var html bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&html, "ticker_widget.tmpl", page); err != nil {
		if app.Err != nil {
			app.Err.Printf("ticker widget: %s", err)
		}
		http.Error(w, "Ticker unavailable", http.StatusInternalServerError)
		return
	}
	// These routes intentionally expose only public data and allow framing by
	// streaming tools. They do not load account navigation or private controls.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "frame-ancestors *")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Write(html.Bytes())
}
