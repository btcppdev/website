package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

type RecordingWatchPage struct {
	Recording      *types.Recording
	ConfTalk       *types.ConfTalk
	Conf           *types.Conf
	Speakers       []*types.Speaker
	Title          string
	Description    string
	Path           string
	SocialImage    string
	YouTubeURL     string
	YouTubeEmbed   template.URL
	HLSURL         template.URL
	XBroadcastURL  string
	State          string
	PublishAt      *time.Time
	PublishLabel   string
	PublishRFC3339 string
	Year           uint
}

type liveSpeakerLink struct {
	Provider string `json:"provider"`
	Handle   string `json:"handle"`
	URL      string `json:"url"`
	Name     string `json:"name,omitempty"`
}

type liveStatusResponse struct {
	Live     bool              `json:"live"`
	WatchURL string            `json:"watch_url,omitempty"`
	Title    string            `json:"title,omitempty"`
	Speakers []liveSpeakerLink `json:"speakers,omitempty"`
}

type liveStatusCache struct {
	mu        sync.Mutex
	response  liveStatusResponse
	expiresAt time.Time
}

const liveStatusCacheTTL = 10 * time.Second

var siteLiveStatusCache liveStatusCache

func (c *liveStatusCache) get(now time.Time, load func() (liveStatusResponse, error)) (liveStatusResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.expiresAt) {
		return c.response, nil
	}
	response, err := load()
	if err != nil {
		return liveStatusResponse{}, err
	}
	c.response = response
	c.expiresAt = now.Add(liveStatusCacheTTL)
	return response, nil
}

func liveTickerTitle(title string) string {
	runes := []rune(strings.TrimSpace(title))
	if len(runes) <= 50 {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:47])) + "..."
}

func liveTickerSpeakerLinks(speakers []*types.Speaker) []liveSpeakerLink {
	links := make([]liveSpeakerLink, 0, len(speakers))
	for _, speaker := range speakers {
		if speaker == nil {
			continue
		}
		if handle := strings.TrimSpace(speaker.Twitter.Handle); handle != "" {
			links = append(links, liveSpeakerLink{Provider: "X", Handle: strings.TrimPrefix(handle, "@"), URL: speaker.Twitter.Link()})
			continue
		}
		if githubProfileURL := profileURL(speaker.Github, "github.com"); githubProfileURL != "" {
			if parsed, err := url.Parse(githubProfileURL); err == nil {
				host := strings.ToLower(parsed.Hostname())
				handle := strings.Split(strings.Trim(parsed.Path, "/"), "/")[0]
				if (host == "github.com" || host == "www.github.com") && handle != "" {
					links = append(links, liveSpeakerLink{Provider: "GitHub", Handle: handle, URL: githubProfileURL})
					continue
				}
			}
		}
		if name := strings.TrimSpace(speaker.Name); name != "" {
			links = append(links, liveSpeakerLink{Provider: "Name", Name: name})
		}
	}
	return links
}

func recordingWatchPath(recordingID string) string {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return ""
	}
	return "/watch/" + url.PathEscape(recordingID)
}

func recordingWatchURL(baseURI, recordingID string) string {
	path := recordingWatchPath(recordingID)
	if path == "" {
		return strings.TrimRight(strings.TrimSpace(baseURI), "/")
	}
	return strings.TrimRight(strings.TrimSpace(baseURI), "/") + path
}

func recordingWatchState(rec *types.Recording, now time.Time) string {
	if rec != nil && rec.PublishAt != nil && rec.PublishAt.After(now) {
		return "scheduled"
	}
	if rec != nil && youtubeVideoID(rec.YTLink) != "" {
		return "available"
	}
	return "processing"
}

func recordingBroadcastIsLive(broadcast *types.RecordingBroadcast, now time.Time) bool {
	return broadcast != nil && broadcast.State == "live" && isHTTPURL(broadcast.HLSURL) &&
		broadcast.HeartbeatAt != nil && broadcast.HeartbeatAt.After(now.Add(-2*time.Minute))
}

func recordingWatchDescription(ct *types.ConfTalk, conf *types.Conf) string {
	if ct != nil && ct.Proposal != nil {
		if description := strings.TrimSpace(ct.Proposal.Description); description != "" {
			const maxRunes = 180
			runes := []rune(strings.Join(strings.Fields(description), " "))
			if len(runes) > maxRunes {
				return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
			}
			return string(runes)
		}
	}
	if conf != nil {
		return fmt.Sprintf("Watch this talk from %s on bitcoin++.", conf.Desc)
	}
	return "Watch this bitcoin++ talk."
}

func recordingWatchSocialImage(ctx *config.AppContext, row *RecordingRow, conf *types.Conf) string {
	key := recordingNotificationCardKey(row, conf)
	if key != "" && spaces.IsConfigured() {
		return spaces.PublicURL(key)
	}
	if row != nil && row.ConfTalk != nil && conf != nil {
		return fmt.Sprintf("/media/png/%s/talk/1080p/%s", url.PathEscape(conf.Tag), url.PathEscape(row.ConfTalk.ID))
	}
	if conf != nil {
		return confSocialImage(conf.Tag, "twitter")
	}
	return "/static/img/rebrand/breakthroughs.jpg"
}

func RecordingWatch(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	recordingID := strings.TrimSpace(mux.Vars(r)["recordingID"])
	rec, err := getters.GetRecordingByID(ctx, recordingID)
	if err != nil {
		ctx.Err.Printf("watch recording %q: %s", recordingID, err)
		http.Error(w, "Unable to load this recording, please try again later", http.StatusInternalServerError)
		return
	}
	if rec == nil {
		handle404(w, r, ctx)
		return
	}
	confTalk, err := getters.GetConfTalkByID(ctx, rec.ConfTalkID)
	if err != nil {
		ctx.Err.Printf("watch recording %s talk %s: %s", rec.ID, rec.ConfTalkID, err)
		http.Error(w, "Unable to load this recording, please try again later", http.StatusInternalServerError)
		return
	}
	if confTalk == nil || confTalk.Conf == nil || !confTalk.Conf.IsPublished() {
		handle404(w, r, ctx)
		return
	}
	row := &RecordingRow{Recording: rec, ConfTalk: confTalk}
	if confTalk.Proposal != nil {
		row.Speakers = recordingSpeakersForProposal(confTalk.Proposal, ctx)
	}
	conf := confTalk.Conf
	title := strings.TrimSpace(rec.TalkName)
	if row.ConfTalk.Proposal != nil && strings.TrimSpace(row.ConfTalk.Proposal.Title) != "" {
		title = strings.TrimSpace(row.ConfTalk.Proposal.Title)
	}
	if title == "" {
		title = "bitcoin++ talk"
	}

	state := recordingWatchState(rec, time.Now())
	broadcast, broadcastErr := getters.GetRecordingBroadcast(ctx, rec.ID)
	if broadcastErr != nil {
		ctx.Err.Printf("watch recording %s broadcast: %s", rec.ID, broadcastErr)
	}
	if recordingBroadcastIsLive(broadcast, time.Now()) {
		state = "live"
	}
	page := &RecordingWatchPage{
		Recording:   rec,
		ConfTalk:    row.ConfTalk,
		Conf:        conf,
		Speakers:    row.Speakers,
		Title:       title,
		Description: recordingWatchDescription(row.ConfTalk, conf),
		Path:        recordingWatchPath(rec.ID),
		SocialImage: recordingWatchSocialImage(ctx, row, conf),
		YouTubeURL:  strings.TrimSpace(rec.YTLink),
		State:       state,
		PublishAt:   rec.PublishAt,
		Year:        helpers.CurrentYear(),
	}
	if broadcast != nil {
		if isHTTPURL(broadcast.HLSURL) {
			page.HLSURL = template.URL(broadcast.HLSURL)
		}
		page.XBroadcastURL = strings.TrimSpace(broadcast.XBroadcastURL)
	}
	if videoID := youtubeVideoID(rec.YTLink); videoID != "" {
		page.YouTubeEmbed = template.URL("https://www.youtube-nocookie.com/embed/" + url.PathEscape(videoID) + "?playsinline=1")
	}
	if rec.PublishAt != nil {
		loc := conf.Loc()
		page.PublishLabel = rec.PublishAt.In(loc).Format("Monday, January 2 at 3:04 PM MST")
		page.PublishRFC3339 = rec.PublishAt.UTC().Format(time.RFC3339)
	}

	if err := ctx.TemplateCache.ExecuteTemplate(w, "watch.tmpl", page); err != nil {
		ctx.Err.Printf("watch recording %s template: %s", rec.ID, err)
		http.Error(w, "Unable to load this recording, please try again later", http.StatusInternalServerError)
	}
}

func RecordingWatchStatus(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	recordingID := strings.TrimSpace(mux.Vars(r)["recordingID"])
	rec, err := getters.GetRecordingByID(ctx, recordingID)
	if err != nil {
		http.Error(w, `{"error":"status unavailable"}`, http.StatusInternalServerError)
		return
	}
	if rec == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	state := recordingWatchState(rec, time.Now())
	broadcast, err := getters.GetRecordingBroadcast(ctx, recordingID)
	if err == nil && recordingBroadcastIsLive(broadcast, time.Now()) {
		state = "live"
	}
	response := struct {
		State  string `json:"state"`
		HLSURL string `json:"hls_url,omitempty"`
	}{State: state}
	if state == "live" && isHTTPURL(broadcast.HLSURL) {
		response.HLSURL = broadcast.HLSURL
	}
	_ = json.NewEncoder(w).Encode(response)
}

func LiveStatus(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=5, s-maxage=15, stale-while-revalidate=30")

	now := time.Now()
	response, err := siteLiveStatusCache.get(now, func() (liveStatusResponse, error) {
		return loadLiveStatus(ctx, now)
	})
	if err != nil {
		ctx.Err.Printf("live status: %s", err)
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, `{"error":"status unavailable"}`, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(response)
}

func loadLiveStatus(ctx *config.AppContext, now time.Time) (liveStatusResponse, error) {
	broadcast, err := getters.GetActiveRecordingBroadcast(ctx, now.Add(-2*time.Minute))
	if err != nil {
		return liveStatusResponse{}, err
	}
	response := liveStatusResponse{}
	if broadcast == nil || !recordingBroadcastIsLive(broadcast, now) {
		return response, nil
	}
	rec, err := getters.GetRecordingByID(ctx, broadcast.RecordingID)
	if err != nil {
		return liveStatusResponse{}, fmt.Errorf("recording %s: %w", broadcast.RecordingID, err)
	}
	if rec == nil {
		return response, nil
	}
	confTalk, err := getters.GetConfTalkByID(ctx, rec.ConfTalkID)
	if err != nil {
		return liveStatusResponse{}, fmt.Errorf("recording %s talk: %w", rec.ID, err)
	}
	if confTalk == nil || confTalk.Conf == nil || !confTalk.Conf.IsPublished() {
		return response, nil
	}
	response.Live = true
	response.WatchURL = recordingWatchPath(rec.ID)
	response.Title = strings.TrimSpace(rec.TalkName)
	if confTalk.Proposal != nil && strings.TrimSpace(confTalk.Proposal.Title) != "" {
		response.Title = strings.TrimSpace(confTalk.Proposal.Title)
	}
	response.Title = liveTickerTitle(response.Title)
	if confTalk.Proposal != nil {
		response.Speakers = liveTickerSpeakerLinks(recordingSpeakersForProposal(confTalk.Proposal, ctx))
	}
	return response, nil
}
