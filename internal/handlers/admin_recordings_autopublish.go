package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	youtubepkg "btcpp-web/external/youtube"
	"btcpp-web/internal/config"
)

const (
	recordingStatusPending      = "pending"
	recordingStatusUploading    = "uploading"
	recordingStatusUploaded     = "uploaded"
	recordingStatusScheduling   = "scheduling"
	recordingStatusScheduled    = "scheduled"
	recordingStatusPosting      = "posting"
	recordingStatusPosted       = "posted"
	recordingStatusFailed       = "failed"
	recordingStatusAuthRequired = "auth_required"
)

// StartRecordingAutopublisher uploads scheduled recordings to YouTube. X
// broadcasts are created explicitly from the recording admin because X's
// private create endpoint is non-idempotent and requires durable step state.
func StartRecordingAutopublisher(ctx *config.AppContext) {
	if ctx == nil || ctx.Env == nil || !ctx.Env.Recordings.AutopublishEnabled {
		return
	}
	go func() {
		wait := time.Duration(ctx.Env.Recordings.PollSec) * time.Second
		if wait <= 0 {
			wait = time.Minute
		}
		ctx.Infos.Printf("recording autopublisher enabled; polling every %s", wait)
		time.Sleep(5 * time.Second)
		for {
			runRecordingAutopublishTick(ctx)
			time.Sleep(wait)
		}
	}()
}

func runRecordingAutopublishTick(ctx *config.AppContext) {
	if !youtubepkg.IsConfigured() || !youtubepkg.IsConnected() || !youtubepkg.UpdatesEnabled() {
		return
	}
	recs, err := getters.ListRecordingsReadyForYouTube(ctx)
	if err != nil {
		ctx.Err.Printf("recording autopublisher recordings: %s", err)
		return
	}
	for _, row := range recordingRowsFromList(ctx, recs, "") {
		if row == nil || row.Recording == nil {
			continue
		}
		if shouldUploadRecordingToYouTube(row) {
			runScheduledYouTubeUpload(ctx, row)
		}
	}
}

func shouldUploadRecordingToYouTube(row *RecordingRow) bool {
	if row == nil || row.Recording == nil {
		return false
	}
	if row.YTURL != "" || row.Recording.FileURI == "" {
		return false
	}
	return statusAllowsRetry(row.YTStatus)
}

func statusAllowsRetry(status string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	return status == "" || status == recordingStatusPending || status == "queued"
}

func runScheduledYouTubeUpload(ctx *config.AppContext, row *RecordingRow) {
	rec := row.Recording
	title, body := defaultYouTubeCopy(ctx, row)
	if title == "" {
		title = rec.TalkName
	}
	status := recordingStatusUploading
	if err := upsertRecordingSocialPost(ctx, row, recordingPlatformYouTube, getters.SocialPostUpdate{Text: &body, Status: &status}); err != nil {
		ctx.Err.Printf("recording autopublish yt status recording=%s: %s", rec.ID, err)
	}

	privacy := "public"
	var publishAt time.Time
	if rec.PublishAt != nil && rec.PublishAt.After(time.Now()) {
		privacy = "private"
		publishAt = rec.PublishAt.UTC()
	}
	src, size, err := openRecordingSourceStream(rec.FileURI)
	if err != nil {
		recordYouTubeFailure(ctx, row, "couldn't fetch source video from Spaces: "+err.Error())
		return
	}
	defer src.Close()

	ytURL, err := youtubepkg.Upload(context.Background(), youtubepkg.UploadParams{
		Title: title, Description: body, PrivacyStatus: privacy, PublishAt: publishAt,
	}, src, size)
	if err != nil {
		recordYouTubeFailure(ctx, row, err.Error())
		return
	}
	now := time.Now()
	status = recordingStatusUploaded
	if err := getters.UpdateRecordingYTLink(ctx, rec.ID, ytURL); err != nil {
		ctx.Err.Printf("recording autopublish persist yt recording=%s: %s", rec.ID, err)
		return
	}
	rec.YTLink = ytURL
	if err := uploadRecordingYouTubeThumbnail(ctx, context.Background(), rec); err != nil {
		ctx.Err.Printf("recording autopublish thumbnail recording=%s: %s", rec.ID, err)
	}
	if row.ConfTalk != nil && row.ConfTalk.Conf != nil {
		if err := addRecordingToYouTubePlaylist(context.Background(), rec.ID, ytURL, row.ConfTalk.Conf.YouTubePlaylistID); err != nil {
			ctx.Err.Printf("recording autopublish playlist recording=%s playlist=%s: %s", rec.ID, row.ConfTalk.Conf.YouTubePlaylistID, err)
		}
	}
	if err := upsertRecordingSocialPost(ctx, row, recordingPlatformYouTube, getters.SocialPostUpdate{URL: &ytURL, Status: &status, PostedAt: &now}); err != nil {
		ctx.Err.Printf("recording autopublish persist yt socialpost recording=%s: %s", rec.ID, err)
		return
	}
	ctx.Infos.Printf("recording autopublish yt uploaded recording=%s url=%s", rec.ID, ytURL)
}

func recordYouTubeFailure(ctx *config.AppContext, row *RecordingRow, msg string) {
	rec := row.Recording
	status := recordingStatusFailed
	if err := upsertRecordingSocialPost(ctx, row, recordingPlatformYouTube, getters.SocialPostUpdate{Status: &status, Error: &msg}); err != nil {
		ctx.Err.Printf("recording autopublish persist yt failure recording=%s: %s", rec.ID, err)
	}
	ctx.Err.Printf("recording autopublish yt failed recording=%s: %s", rec.ID, msg)
}

func openRecordingSourceStream(fileURI string) (io.ReadCloser, int64, error) {
	key := recordingSourceObjectKey(fileURI)
	if key == "" {
		return nil, 0, fmt.Errorf("empty FileURI")
	}
	src, size, err := spaces.GetStream(key)
	if err != nil {
		return nil, 0, fmt.Errorf("key %q: %w", key, err)
	}
	return src, size, nil
}

func recordingSourceObjectKey(fileURI string) string {
	raw := strings.TrimSpace(fileURI)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		path := strings.TrimPrefix(parsed.EscapedPath(), "/")
		if unescaped, err := url.PathUnescape(path); err == nil {
			path = unescaped
		}
		return path
	}
	return strings.TrimPrefix(raw, "/")
}

func redirectRecordingsListErr(w http.ResponseWriter, r *http.Request, confTag, msg string) {
	http.Redirect(w, r, recordingsAdminPath(confTag, "?err="+url.QueryEscape(msg)), http.StatusSeeOther)
}
