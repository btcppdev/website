package handlers

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

func conferenceLivePath(tag string) string {
	return "/conf/" + url.PathEscape(tag) + "/live"
}

func conferenceLivePage(conf *types.Conf, broadcast *types.ConferenceBroadcast, now time.Time) *RecordingWatchPage {
	page := &RecordingWatchPage{
		Conf: conf, Title: conf.Desc + " — live", Description: "Watch the livestream from " + conf.Desc + ".",
		Path: conferenceLivePath(conf.Tag), State: "offline", Year: helpers.CurrentYear(), ConferenceWide: true,
		SocialImage: "/static/img/rebrand/breakthroughs.jpg",
	}
	if broadcast != nil {
		if title := strings.TrimSpace(broadcast.Title); title != "" {
			page.Title = title
		}
		if recordingBroadcastIsLive(&broadcast.RecordingBroadcast, now) {
			page.State = "live"
			page.HLSURL = template.URL(broadcast.HLSURL)
		}
		if isHTTPURL(broadcast.XBroadcastURL) {
			page.XBroadcastURL = broadcast.XBroadcastURL
		}
	}
	return page
}

func ConferenceLive(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	page, ok := loadConferenceLivePage(w, r, ctx)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "watch.tmpl", page); err != nil {
		ctx.Err.Printf("conference live template: %s", err)
	}
}

func ConferenceLiveStatus(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	page, ok := loadConferenceLivePage(w, r, ctx)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		State  string `json:"state"`
		HLSURL string `json:"hls_url,omitempty"`
	}{page.State, string(page.HLSURL)})
}

func loadConferenceLivePage(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*RecordingWatchPage, bool) {
	conf, err := getters.GetConfByTag(ctx, mux.Vars(r)["conf"])
	if err != nil {
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return nil, false
	}
	if conf == nil || !conf.IsPublished() {
		handle404(w, r, ctx)
		return nil, false
	}
	broadcast, err := getters.GetConferenceBroadcast(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load broadcast", http.StatusInternalServerError)
		return nil, false
	}
	return conferenceLivePage(conf, broadcast, time.Now()), true
}

func loadConferenceLiveStatus(ctx *config.AppContext, now time.Time) (liveStatusResponse, error) {
	broadcast, err := getters.GetActiveConferenceBroadcast(ctx, now.Add(-2*time.Minute))
	if err != nil {
		return liveStatusResponse{}, err
	}
	if broadcast == nil || !recordingBroadcastIsLive(&broadcast.RecordingBroadcast, now) {
		return liveStatusResponse{}, nil
	}
	conf, err := getters.GetConfByRef(ctx, broadcast.ConferenceID)
	if err != nil {
		return liveStatusResponse{}, err
	}
	if conf == nil || !conf.IsPublished() {
		return liveStatusResponse{}, nil
	}
	page := conferenceLivePage(conf, broadcast, now)
	return liveStatusResponse{Live: true, WatchURL: page.Path, Title: liveTickerTitle(page.Title)}, nil
}
