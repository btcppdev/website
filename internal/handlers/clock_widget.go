package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
)

type clockWidgetPage struct {
	Timezone string `json:"timezone"`
	Time     string `json:"-"`
}

// Use the same next-up conference as the website's header ticker.
func clockWidgetData(nav NavConfList, now time.Time) clockWidgetPage {
	loc := time.UTC
	if len(nav.Upcoming) > 0 && nav.Upcoming[0] != nil {
		conf := nav.Upcoming[0]
		name := strings.TrimSpace(conf.Timezone)
		if name == "" {
			name = conf.Loc().String()
		}
		if candidate, err := time.LoadLocation(name); err == nil && name != "Local" {
			loc = candidate
		}
	}
	return clockWidgetPage{Timezone: loc.String(), Time: now.In(loc).Format("15:04:05")}
}

func registerClockWidget(router *mux.Router, app *config.AppContext) {
	router.HandleFunc("/widgets/clock", func(w http.ResponseWriter, r *http.Request) {
		page := clockWidgetData(buildNavConfList(app), time.Now())
		var html bytes.Buffer
		if err := app.TemplateCache.ExecuteTemplate(&html, "clock_widget.tmpl", page); err != nil {
			if app.Err != nil {
				app.Err.Printf("clock widget: %s", err)
			}
			http.Error(w, "Clock unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "frame-ancestors *")
		w.Header().Set("X-Robots-Tag", "noindex")
		w.Write(html.Bytes())
	}).Methods(http.MethodGet)
	router.HandleFunc("/widgets/clock/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(clockWidgetData(buildNavConfList(app), time.Now()))
	}).Methods(http.MethodGet)
}
