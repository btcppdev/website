package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/xstudio"
	youtubepkg "btcpp-web/external/youtube"
	"btcpp-web/internal/config"
	"btcpp-web/internal/publishing"
)

func applyPublishingRecording(ctx context.Context, app *config.AppContext, d publishing.RecordingChange) error {
	fail := func(message string) error { return &publishing.ScheduleApplyError{Message: message} }
	rec, err := getters.GetRecordingByID(app, d.RecordingID)
	if err != nil {
		return fail("Could not load the recording")
	}
	if rec == nil {
		return fail("Recording no longer exists")
	}
	if (d.TargetAt != nil && (rec.YTLink != d.YouTubeURL || rec.FileURI != d.FileURI)) || (d.ChannelID == "youtube" && rec.YTLink != d.YouTubeURL) {
		return fail("Recording media changed; review the new source before scheduling")
	}
	if d.ChannelID == "youtube" {
		if !youtubepkg.IsConfigured() || !youtubepkg.IsConnected() {
			return fail("Connect YouTube before synchronizing this release")
		}
		if !youtubepkg.UpdatesEnabled() {
			return fail("YouTube updates are disabled on the server")
		}
		id := youtubeVideoID(rec.YTLink)
		if id == "" {
			return fail("The recording has no valid YouTube video")
		}
		status, err := youtubepkg.GetVideoStatus(ctx, id)
		if err != nil {
			return fail("Could not check YouTube: " + err.Error())
		}
		if err = getters.CacheRecordingYouTubeStatus(app, rec.ID, rec.YTLink, status.PrivacyStatus); err != nil {
			return fail("Could not save YouTube visibility")
		}
		if status.PrivacyStatus == "public" {
			return fail("This video is already public on YouTube; it cannot be rescheduled")
		}
		if d.TargetAt == nil {
			if status.PublishAt == nil && status.PrivacyStatus == "unlisted" {
				return nil
			}
			if err = youtubepkg.ClearExistingVideoSchedule(ctx, id); err != nil {
				return err
			}
		} else {
			if status.PublishAt != nil && status.PublishAt.Equal(*d.TargetAt) {
				return nil
			}
			if err = youtubepkg.ScheduleExistingVideo(ctx, id, *d.TargetAt); err != nil {
				return err
			}
		}
		return youtubepkg.WaitForVideoSchedule(ctx, id, d.TargetAt)
	}
	if d.ChannelID != "broadcasts" {
		return fail("Unsupported recording destination")
	}
	if d.TargetAt == nil {
		operation, err := getters.GetRecordingXBroadcast(app, rec.ID)
		if err != nil {
			return fail("Could not load the existing broadcast")
		}
		if d.BroadcastID == "" && operation == nil {
			return nil // Never sent to X.
		}
		if operation == nil || operation.BroadcastID != d.BroadcastID {
			return fail("The X broadcast changed; review its schedule before canceling")
		}
		if operation.Status == "canceled" {
			return nil
		}
		client, err := publishingXStudio(app)
		if err != nil {
			return fail(err.Error())
		}
		return client.CancelScheduled(ctx, operation.SessionID, d.BroadcastID)
	}
	if !app.Env.Recordings.X.Enabled {
		return fail("X broadcast scheduling is disabled on the server")
	}
	row := buildRecordingRow(app, rec)
	if row == nil || row.ConfTalk == nil || row.ConfTalk.Conf == nil {
		return fail("The recording must belong to an event")
	}
	existing, err := getters.GetRecordingXBroadcast(app, rec.ID)
	if err != nil {
		return fail("Could not load the existing broadcast")
	}
	attachRecordingXBroadcast(row, existing)
	work, disposition, err := claimRecordingXBroadcast(app, row.ConfTalk.Conf, rec, row, *d.TargetAt)
	if err != nil {
		return fail(err.Error())
	}
	if disposition == "running" {
		return &publishing.ScheduleApplyError{Message: "X broadcast setup is already in progress. Check its status before retrying.", Review: true}
	}
	if work != nil {
		runRecordingXBroadcastWork(app, work)
	}
	operation, err := getters.GetRecordingXBroadcast(app, rec.ID)
	if err != nil {
		return err
	}
	if operation == nil || operation.Status != "scheduled" || !operation.ScheduledAt.Equal(*d.TargetAt) {
		message := "X has not confirmed this broadcast schedule"
		if operation != nil && operation.Error != "" {
			message += ": " + operation.Error
		}
		return &publishing.ScheduleApplyError{Message: message, Review: true}
	}
	return nil
}
func StartRecordingPublishingWorker(app *config.AppContext) {
	if app.DB == nil || !app.Env.Publishing.Enabled {
		return
	}
	worker := publishing.RecordingScheduler{Store: publishing.Store{DB: app.DB}, Confirm: func(ctx context.Context, d publishing.RecordingChange) (bool, error) {
		if d.ChannelID == "youtube" {
			return confirmPublishingYouTube(ctx, d)
		}
		if d.TargetAt != nil || d.BroadcastID == "" {
			return false, nil
		}
		client, err := publishingXStudio(app)
		if err != nil {
			return false, err
		}
		state, err := client.ScheduledBroadcastState(ctx, "", d.BroadcastID)
		return err == nil && state == "absent", err
	}, Apply: func(ctx context.Context, d publishing.RecordingChange) error {
		return applyPublishingRecording(ctx, app, d)
	}}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			err := worker.RunOnce(ctx)
			cancel()
			if err != nil {
				app.Err.Printf("recording schedule synchronization: %s", err)
			}
		}
	}()
}

func publishingXStudio(app *config.AppContext) (*xstudio.Client, error) {
	if !app.Env.Recordings.X.Enabled {
		return nil, fmt.Errorf("X broadcast scheduling is disabled on the server")
	}
	return xstudio.New(xstudio.Config{Cookie: app.Env.Recordings.X.Cookie, UserAgent: app.Env.Recordings.X.UserAgent, IngestID: app.Env.Recordings.X.IngestID})
}

func confirmPublishingYouTube(ctx context.Context, d publishing.RecordingChange) (bool, error) {
	status, err := youtubepkg.GetVideoStatus(ctx, youtubeVideoID(d.YouTubeURL))
	return err == nil && status.MatchesSchedule(d.TargetAt), err
}

// Old scheduling bookmarks land at the unified scheduler. POSTs do not replay
// unreviewed legacy timestamps after the migration.
func redirectRecordingPublishing(w http.ResponseWriter, r *http.Request, event, recording, channel string) {
	values := url.Values{"event": {event}, "recordings": {"1"}}
	if recording != "" {
		values.Set("recording", recording)
	}
	if channel != "" {
		values.Set("recording_channel", channel)
	}
	http.Redirect(w, r, "/admin/publishing?"+values.Encode()+"#week", http.StatusSeeOther)
}
func isRecordingPublishingID(id string) bool {
	return strings.HasPrefix(id, "youtube:") || strings.HasPrefix(id, "broadcast:")
}

func refreshPublishingRecordingStatus(app *config.AppContext, event string) error {
	if !youtubepkg.IsConfigured() || !youtubepkg.IsConnected() {
		return fmt.Errorf("Connect YouTube before refreshing recording status")
	}
	recs, err := getters.ListRecordingsForConf(app, event)
	if event == "" {
		recs, err = getters.ListRecordings(app)
	}
	if err != nil {
		return err
	}
	rows := make([]*RecordingRow, 0, len(recs))
	for _, rec := range recs {
		rows = append(rows, &RecordingRow{Recording: rec})
	}
	if err = refreshRecordingScheduleDurations(app, rows); err != nil {
		return err
	}
	for _, row := range rows {
		if row.YTStatusError != "" {
			return fmt.Errorf("%s: %s", row.Recording.TalkName, row.YTStatusError)
		}
	}
	return nil
}
