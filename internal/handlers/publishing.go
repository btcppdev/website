package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/secureblob"
	"btcpp-web/external/spaces"
	"btcpp-web/external/xapi"
	youtubepkg "btcpp-web/external/youtube"
	"btcpp-web/internal/config"
	"btcpp-web/internal/publishing"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
)

func registerPublishingRoutes(r *mux.Router, app *config.AppContext) {
	for _, path := range []string{"/admin/publishing", "/admin/publishing/data", "/admin/publishing/slots", "/admin/publishing/action", "/admin/publishing/media", "/admin/publishing/connect-x", "/admin/publishing/x-callback"} {
		r.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { AdminPublishing(w, r, app) }).Methods("GET", "POST")
	}
}
func publishingOAuth(app *config.AppContext) *oauth2.Config {
	c := app.Env.Publishing
	return xapi.Config(c.XClientID, c.XClientSecret, c.XRedirectURL)
}
func publishingXConfigured(app *config.AppContext) bool {
	c := app.Env.Publishing
	_, e := secureblob.DecodeKey(app.Env.Recordings.EncryptionKey)
	return c.XClientID != "" && c.XClientSecret != "" && c.XRedirectURL != "" && e == nil
}
func publishingJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func AdminPublishing(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	if requireGlobalAdmin(w, r, app) == nil {
		return
	}
	if app.DB == nil {
		http.Error(w, "Database unavailable", 503)
		return
	}
	store := publishing.Store{DB: app.DB}
	if r.Method == "POST" {
		if !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.Header.Get("X-CSRF-Token")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		if r.URL.Path == "/admin/publishing/media" {
			publishingMedia(w, r, app)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var input struct {
			Action              string
			Event               string
			Revision            string
			Excluded            bool
			IDs                 []string
			Moves               []publishing.RecordingMove
			ConfirmedNotCreated bool     `json:"confirmed_not_created"`
			ChannelIDs          []string `json:"channel_ids"`
			TargetChannel       string   `json:"target_channel"`
			At                  time.Time
			Delivery            publishing.Delivery
			Channel             publishing.Channel
			ID                  string
			Version             int
			URL                 string
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if dec.Decode(&input) != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		var err error
		var result any = map[string]bool{"ok": true}
		switch input.Action {
		case "save":
			for _, m := range input.Delivery.Media {
				if !publishingMediaURL(m.URL) {
					http.Error(w, "Choose an uploaded image", 400)
					return
				}
			}
			input.Delivery.CampaignID = "" // Campaign identity is generated on the server.
			input.Delivery.SourceRef = ""  // Event references are assigned only by trusted workflows.
			if input.Delivery.ID == "" {
				if _, e := uuid.Parse(input.Delivery.ClientKey); e != nil {
					http.Error(w, "Reload the composer before saving", 400)
					return
				}
				input.Delivery.SourceRef = "composer:" + input.Delivery.ClientKey
			}
			if len(input.ChannelIDs) > 0 {
				var ids []string
				ids, err = store.SaveCampaign(r.Context(), input.Delivery, input.ChannelIDs)
				result = map[string]any{"ids": ids}
			} else {
				var id string
				id, err = store.Save(r.Context(), input.Delivery)
				result = map[string]string{"id": id}
			}
		case "move":
			err = store.MoveToSlot(r.Context(), input.ID, input.Version, input.TargetChannel, input.At)

		case "recording_refresh":
			err = refreshPublishingRecordingStatus(app, input.Event)
		case "recording_preview":
			result, err = store.PreviewRecordingQueue(r.Context(), input.IDs)
		case "recording_exclusion":
			err = store.SetRecordingExcluded(r.Context(), input.ID, input.Revision, input.Excluded)
		case "recording_apply":
			err = store.SaveRecordingMoves(r.Context(), input.Moves)
		case "recording_move":
			expected := "youtube"
			if strings.HasPrefix(input.ID, "broadcast:") {
				expected = "broadcasts"
			}
			if !isRecordingPublishingID(input.ID) || input.TargetChannel != expected {
				err = fmt.Errorf("Choose a slot for this same recording channel")
			} else {
				err = store.SaveRecordingMoves(r.Context(), []publishing.RecordingMove{{ID: input.ID, Revision: input.Revision, At: &input.At}})
			}
		case "recording_clear":
			err = store.SaveRecordingMoves(r.Context(), []publishing.RecordingMove{{ID: input.ID, Revision: input.Revision}})
		case "recording_retry":
			err = store.RetryRecordingChange(r.Context(), input.ID, input.Revision, input.ConfirmedNotCreated)
		case "channel":
			if input.Channel.ID == "youtube" {
				err = publishing.ValidateSlots(input.Channel.Timezone, input.Channel.Slots)
				if err == nil {
					var slots []*types.YouTubePublishSlot
					for _, v := range input.Channel.Slots {
						slots = append(slots, &types.YouTubePublishSlot{Weekday: time.Weekday(v.Day), TimeOfDay: v.Time, Timezone: input.Channel.Timezone, Active: true})
					}
					err = getters.ReplaceYouTubePublishSlots(app, slots)
				}
			} else if input.Channel.ID == "broadcasts" {
				err = store.SaveBroadcastSlots(r.Context(), input.Channel)
			} else {
				err = store.SaveChannel(r.Context(), input.Channel)
			}
		default:
			err = store.Action(r.Context(), input.ID, input.Version, input.Action, input.URL)
		}
		if err != nil {
			http.Error(w, publishingError(err), 400)
			return
		}
		publishingJSON(w, result)
		return
	}
	switch r.URL.Path {
	case "/admin/publishing/connect-x":
		if !publishingXConfigured(app) {
			http.Error(w, "Set X_OAUTH_CLIENT_ID, X_OAUTH_CLIENT_SECRET, X_OAUTH_REDIRECT_URL and SOCIAL_STATE_KEY on the server first", 503)
			return
		}
		state := uuid.NewString()
		verifier := oauth2.GenerateVerifier()
		app.Session.Put(r.Context(), "publishing_x_state", state)
		app.Session.Put(r.Context(), "publishing_x_verifier", verifier)
		app.Session.Put(r.Context(), "publishing_x_started", time.Now().Unix())
		http.Redirect(w, r, publishingOAuth(app).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusSeeOther)
	case "/admin/publishing/x-callback":
		state := app.Session.PopString(r.Context(), "publishing_x_state")
		verifier := app.Session.PopString(r.Context(), "publishing_x_verifier")
		started := app.Session.GetInt64(r.Context(), "publishing_x_started")
		app.Session.Remove(r.Context(), "publishing_x_started")
		if state == "" || !secureTokenEqual(state, r.URL.Query().Get("state")) || verifier == "" || time.Now().Unix()-started > 600 {
			http.Error(w, "X connection expired; try again", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		token, e := publishingOAuth(app).Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
		if e != nil {
			http.Error(w, "X authorization failed; try connecting again", 502)
			return
		}
		id, name, e := (xapi.Client{}).Me(ctx, token.AccessToken)
		if e != nil {
			http.Error(w, "Unable to verify the X account", 502)
			return
		}
		key, e := secureblob.DecodeKey(app.Env.Recordings.EncryptionKey)
		if e != nil {
			http.Error(w, "Token encryption is not configured", 503)
			return
		}
		raw, _ := json.Marshal(token)
		encrypted, e := secureblob.Encrypt(raw, key)
		if e == nil {
			e = store.ConnectX(ctx, id, name, encrypted)
		}
		if e != nil {
			http.Error(w, "Could not save account connection", 503)
			return
		}
		http.Redirect(w, r, "/admin/publishing#channels", http.StatusSeeOther)
	case "/admin/publishing/slots":
		var slots []publishing.Entry
		var err error
		if isRecordingPublishingID(r.URL.Query().Get("id")) {
			slots, err = store.AvailableRecordingSlots(r.Context(), r.URL.Query().Get("id"), time.Now())
		} else {
			slots, err = store.AvailableSlots(r.Context(), r.URL.Query().Get("id"), time.Now())
		}
		if err != nil {
			http.Error(w, publishingError(err), 400)
			return
		}
		publishingJSON(w, slots)
	case "/admin/publishing/data":
		start, e := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
		if e != nil {
			http.Error(w, "Invalid range start", 400)
			return
		}
		end, e := time.Parse(time.RFC3339, r.URL.Query().Get("end"))
		if e != nil {
			http.Error(w, "Invalid range end", 400)
			return
		}
		entries, e := store.Calendar(r.Context(), start, end)
		if e != nil {
			app.Err.Printf("publishing calendar: %s", e)
			http.Error(w, "Calendar unavailable", 503)
			return
		}
		channels, e := store.Channels(r.Context())
		if e != nil {
			http.Error(w, "Channels unavailable", 503)
			return
		}
		deliveries, e := store.Deliveries(r.Context())
		if e != nil {
			http.Error(w, "Queue unavailable", 503)
			return
		}
		ytSlots, ytErr := getters.ListYouTubePublishSlots(app)
		if ytErr != nil {
			http.Error(w, "YouTube schedules unavailable", 503)
			return
		}
		for i := range channels {
			if channels[i].ID == "youtube" {
				for _, slot := range ytSlots {
					if slot.Active {
						channels[i].Timezone = slot.Timezone
						channels[i].Slots = append(channels[i].Slots, publishing.Slot{Day: int(slot.Weekday), Time: slot.TimeOfDay})
					}
				}
			}
		}
		var streamerSynced *time.Time
		_ = app.DB.QueryRow(r.Context(), `SELECT synced_at FROM publishing_sync WHERE provider='streamer'`).Scan(&streamerSynced)
		slots := []publishing.Entry{}
		for _, c := range channels {
			if c.Provider == "recordings" {
				generated, slotErr := store.RecordingSlotEntries(r.Context(), c.ID, start, end)
				if slotErr != nil {
					http.Error(w, "Recording slots unavailable", 503)
					return
				}
				slots = append(slots, generated...)
				continue
			}
			for _, at := range publishing.SlotsBetween(c.Timezone, c.Slots, start, end) {
				at := at
				slots = append(slots, publishing.Entry{ID: c.ID + at.String(), ChannelID: c.ID, Title: "Default posting slot", Kind: "slot", At: &at, Provider: c.Provider})
			}
		}
		var events []map[string]string
		eventRows, eventErr := app.DB.Query(r.Context(), `SELECT tag,description FROM conferences ORDER BY start_date DESC NULLS LAST LIMIT 2000`)
		if eventErr != nil {
			http.Error(w, "Events unavailable", 503)
			return
		}
		for eventRows.Next() {
			var tag, name string
			if eventErr = eventRows.Scan(&tag, &name); eventErr != nil {
				break
			}
			events = append(events, map[string]string{"tag": tag, "name": name})
		}
		if eventErr == nil {
			eventErr = eventRows.Err()
		}
		eventRows.Close()
		if eventErr != nil {
			http.Error(w, "Events unavailable", 503)
			return
		}
		recordingItems, recordingErr := store.RecordingItems(r.Context())
		if recordingErr != nil {
			http.Error(w, "Recordings unavailable", 503)
			return
		}
		publishingJSON(w, map[string]any{"recordings": recordingItems, "recording_worker_enabled": app.Env.Publishing.Enabled, "events": events, "channels": channels, "entries": entries, "deliveries": deliveries, "slots": slots, "streamer_synced_at": streamerSynced, "worker_enabled": app.Env.Publishing.Enabled && publishingXConfigured(app), "x_configured": publishingXConfigured(app), "youtube_connected": youtubepkg.IsConnected()})
	case "/admin/publishing":
		csrf, e := ensureAuthMethodsCSRF(app, r)
		if e != nil {
			http.Error(w, "Unable to prepare page", 500)
			return
		}
		var out bytes.Buffer
		if e = app.TemplateCache.ExecuteTemplate(&out, "admin/publishing.tmpl", struct{ CSRF string }{csrf}); e != nil {
			app.Err.Printf("publishing template: %s", e)
			http.Error(w, "Unable to render page", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(out.Bytes())
	default:
		http.NotFound(w, r)
	}
}
func publishingError(e error) string {
	// SQL errors contain implementation details; validation messages are safe.
	if strings.Contains(e.Error(), "SQLSTATE") || strings.Contains(e.Error(), "failed to connect") {
		return "Unable to save. Refresh and try again."
	}
	return e.Error()
}
func publishingMediaURL(raw string) bool {
	if !spaces.IsConfigured() {
		return false
	}
	u, e := url.Parse(raw)
	base, e2 := url.Parse(spaces.PublicURL(""))
	return e == nil && e2 == nil && u.Scheme == "https" && u.Host == base.Host && u.User == nil && strings.HasPrefix(u.Path, base.Path) && !strings.Contains(u.Path, "..") && u.RawQuery == ""
}
func readPublishingImage(ctx context.Context, raw string) ([]byte, error) {
	if !publishingMediaURL(raw) {
		return nil, fmt.Errorf("Image must be uploaded to bitcoin++ storage")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, e
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("Unable to load image")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Image is unavailable")
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, (5<<20)+1))
	if e != nil || len(data) > 5<<20 {
		return nil, fmt.Errorf("X images must be at most 5 MB")
	}
	return data, nil
}
func publishingMedia(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if e := r.ParseMultipartForm(6 << 20); e != nil {
		http.Error(w, "Choose an image of at most 5 MB", 400)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, h, e := r.FormFile("file")
	if e != nil {
		http.Error(w, "Choose an image", 400)
		return
	}
	defer f.Close()
	if h.Size > 5<<20 {
		http.Error(w, "Image exceeds 5 MB", 400)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(f, (5<<20)+1))
	if e != nil {
		http.Error(w, "Image unavailable", 400)
		return
	}
	content := http.DetectContentType(raw)
	ext := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp"}[content]
	if ext == "" {
		http.Error(w, "Use JPEG, PNG or WebP", 400)
		return
	}
	key := "social-media/publishing/" + uuid.NewString() + "." + ext
	if _, e = spaces.UploadStream(key, bytes.NewReader(raw), content, int64(len(raw))); e != nil {
		http.Error(w, "Image upload failed", 502)
		return
	}
	publishingJSON(w, publishing.Media{URL: spaces.PublicURL(key), Kind: "image"})
}
func StartPublishingWorker(app *config.AppContext) {
	StartRecordingPublishingWorker(app)
	if !app.Env.Publishing.Enabled || app.DB == nil {
		return
	}
	key, e := secureblob.DecodeKey(app.Env.Recordings.EncryptionKey)
	if e != nil || !publishingXConfigured(app) {
		app.Err.Printf("publishing worker disabled: X authorization configuration is incomplete")
		return
	}
	worker := publishing.Worker{Store: publishing.Store{DB: app.DB}, OAuth: publishingOAuth(app), Key: key, ReadImage: readPublishingImage}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			e := worker.RunOnce(ctx)
			cancel()
			if e != nil {
				app.Err.Printf("publishing dispatcher: %s", e)
			}
		}
	}()
}
