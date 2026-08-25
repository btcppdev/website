package handlers

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/external/xstudio"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/google/uuid"
)

type recordingXBroadcastWork struct {
	recording *types.Recording
	row       *RecordingRow
	cardKey   string
	action    string
	operation *types.RecordingXBroadcast
}

func RecordingsAdminScheduleXBroadcast(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, rec, row, ok := scopedRecordingFromRequest(w, r, ctx)
	if !ok {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		redirectWithErr(w, r, conf.Tag, rec.ID, "couldn't parse form: "+err.Error())
		return
	}
	publishAt := rec.PublishAt
	if raw := strings.TrimSpace(r.FormValue("publish_at")); raw != "" {
		when, err := parseRecordingPublishAt(raw, conf)
		if err != nil {
			redirectWithErr(w, r, conf.Tag, rec.ID, "couldn't parse publish time: "+err.Error())
			return
		}
		publishAt = &when
	}
	if publishAt == nil {
		redirectWithErr(w, r, conf.Tag, rec.ID, "Set a future PublishAt before scheduling an X broadcast")
		return
	}
	if rec.PublishAt == nil || !rec.PublishAt.Equal(*publishAt) {
		if err := getters.UpdateRecordingPublishAt(ctx, rec.ID, publishAt); err != nil {
			redirectWithErr(w, r, conf.Tag, rec.ID, "couldn't update PublishAt: "+err.Error())
			return
		}
		rec.PublishAt = publishAt
	}
	if _, err := updateRecordingYouTubeSchedule(ctx, rec, publishAt); err != nil {
		redirectWithErr(w, r, conf.Tag, rec.ID, "couldn't update YouTube schedule: "+err.Error())
		return
	}

	work, disposition, err := claimRecordingXBroadcast(ctx, conf, rec, row, *publishAt)
	if err != nil {
		redirectWithErr(w, r, conf.Tag, rec.ID, "couldn't claim X broadcast scheduling: "+err.Error())
		return
	}
	if work == nil {
		if disposition == "scheduled" {
			redirectWithErr(w, r, conf.Tag, rec.ID, "This recording already has a scheduled X broadcast")
		} else {
			redirectWithErr(w, r, conf.Tag, rec.ID, "X broadcast scheduling is already running")
		}
		return
	}
	startRecordingXBroadcastWork(ctx, work)
	http.Redirect(w, r, recordingDetailPath(conf.Tag, rec.ID)+"?flash="+url.QueryEscape("X broadcast scheduling started"), http.StatusSeeOther)
}

func claimRecordingXBroadcast(ctx *config.AppContext, conf *types.Conf, rec *types.Recording, row *RecordingRow, publishAt time.Time) (*recordingXBroadcastWork, string, error) {
	if !ctx.Env.Recordings.X.Enabled {
		return nil, "", fmt.Errorf("X Studio broadcast scheduling is disabled")
	}
	if rec == nil || !publishAt.After(time.Now()) {
		return nil, "", fmt.Errorf("set a future PublishAt before scheduling an X broadcast")
	}
	if row == nil || row.ConfTalk == nil {
		return nil, "", fmt.Errorf("the recording must be attached to a conference talk")
	}
	cardKey := recordingNotificationCardKey(row, conf)
	if strings.TrimSpace(cardKey) == "" {
		return nil, "", fmt.Errorf("generate a talk social card before scheduling an X broadcast")
	}
	needsPoster := row.XBroadcast == nil || strings.TrimSpace(row.XBroadcast.PosterMediaID) == ""
	if needsPoster {
		if !spaces.IsConfigured() {
			return nil, "", fmt.Errorf("Spaces is not configured, so the talk social card cannot be loaded")
		}
		if !spaces.Exists(cardKey) {
			return nil, "", fmt.Errorf("talk social card %s is missing from Spaces", cardKey)
		}
	}

	sessionID := uuid.NewString()
	posterURL := "blob:https://studio.x.com/" + uuid.NewString()
	operation, action, err := getters.ClaimRecordingXBroadcastStep(ctx, rec.ID, publishAt, sessionID, posterURL, time.Now())
	if err != nil {
		return nil, "", err
	}
	if action == "" {
		if operation != nil && operation.Status == "scheduled" {
			return nil, "scheduled", nil
		}
		return nil, "running", nil
	}
	return &recordingXBroadcastWork{recording: rec, row: row, cardKey: cardKey, action: action, operation: operation}, "started", nil
}

func startRecordingXBroadcastWork(ctx *config.AppContext, work *recordingXBroadcastWork) {
	if work == nil || work.recording == nil {
		return
	}
	setXJobProgress(work.recording.ID, "running", "Creating scheduled X broadcast", work.action, 10)
	go runRecordingXBroadcastWork(ctx, work)
}

func runRecordingXBroadcastWork(ctx *config.AppContext, work *recordingXBroadcastWork) {
	if work == nil {
		return
	}
	runRecordingXBroadcastSchedule(ctx, work.recording, work.row, work.cardKey, work.action, work.operation)
}

func runRecordingXBroadcastBatch(ctx *config.AppContext, works []*recordingXBroadcastWork) {
	for _, work := range works {
		runRecordingXBroadcastWork(ctx, work)
	}
}

func runRecordingXBroadcastSchedule(ctx *config.AppContext, rec *types.Recording, row *RecordingRow, cardKey, action string, operation *types.RecordingXBroadcast) {
	client, err := xstudio.New(xstudio.Config{
		Cookie: ctx.Env.Recordings.X.Cookie, UserAgent: ctx.Env.Recordings.X.UserAgent,
		IngestID: ctx.Env.Recordings.X.IngestID,
	})
	if err != nil {
		failRecordingXBroadcast(ctx, rec.ID, err)
		return
	}
	for action != "" {
		switch action {
		case "create":
			setXJobProgress(rec.ID, "running", "Creating scheduled broadcast in X Studio", "create", 20)
			created, err := client.Create(context.Background(), xstudio.CreateInput{
				Title: recordingBroadcastTitle(row), StartsAt: operation.ScheduledAt,
				SessionID: operation.SessionID, OptimisticPosterURL: operation.OptimisticPosterURL,
				HighLatency: true, ChatOption: 2,
			})
			if err != nil {
				failRecordingXBroadcast(ctx, rec.ID, err)
				return
			}
			if err := getters.SaveRecordingXBroadcastCreated(ctx, rec.ID, created.ScheduledBroadcastID, created.BroadcastID, time.Now()); err != nil {
				failRecordingXBroadcast(ctx, rec.ID, fmt.Errorf("X created broadcast %s but progress could not be saved: %w", created.BroadcastID, err))
				return
			}
		case "upload_poster":
			setXJobProgress(rec.ID, "running", "Uploading the talk social card to X Studio", "upload_poster", 50)
			poster, err := spaces.Get(cardKey)
			if err != nil {
				failRecordingXBroadcast(ctx, rec.ID, fmt.Errorf("load social card %s: %w", cardKey, err))
				return
			}
			uploaded, err := client.UploadPoster(context.Background(), operation.SessionID, filepath.Base(cardKey), mime.TypeByExtension(filepath.Ext(cardKey)), bytes.NewReader(poster))
			if err != nil {
				failRecordingXBroadcast(ctx, rec.ID, err)
				return
			}
			if err := getters.SaveRecordingXBroadcastPoster(ctx, rec.ID, uploaded.MediaID, time.Now()); err != nil {
				failRecordingXBroadcast(ctx, rec.ID, fmt.Errorf("X uploaded poster %s but progress could not be saved: %w", uploaded.MediaID, err))
				return
			}
		case "finalize":
			setXJobProgress(rec.ID, "running", "Attaching the poster and finalizing the X broadcast", "finalize", 80)
			finalized, err := client.Finalize(context.Background(), xstudio.FinalizeInput{
				ScheduledBroadcastID: operation.ScheduledBroadcastID, BroadcastID: operation.BroadcastID,
				PosterMediaID: operation.PosterMediaID, StartsAt: operation.ScheduledAt, SessionID: operation.SessionID,
			})
			if err != nil {
				failRecordingXBroadcast(ctx, rec.ID, err)
				return
			}
			if err := getters.SaveRecordingXBroadcastFinalized(ctx, rec.ID, finalized.PosterURL, time.Now()); err != nil {
				failRecordingXBroadcast(ctx, rec.ID, fmt.Errorf("X finalized broadcast %s but progress could not be saved: %w", finalized.BroadcastID, err))
				return
			}
			broadcastURL := "https://x.com/i/broadcasts/" + url.PathEscape(finalized.BroadcastID)
			if err := getters.SetRecordingXBroadcastURL(ctx, rec.ID, broadcastURL, time.Now()); err != nil {
				failRecordingXBroadcast(ctx, rec.ID, err)
				return
			}
			if err := getters.UpdateRecordingXLink(ctx, rec.ID, broadcastURL); err != nil {
				ctx.Err.Printf("save recording X broadcast link recording=%s: %s", rec.ID, err)
			}
			setXJobProgress(rec.ID, "succeeded", broadcastURL, "done", 100)
			ctx.Infos.Printf("recording X broadcast scheduled recording=%s broadcast=%s at=%s", rec.ID, finalized.BroadcastID, operation.ScheduledAt.UTC().Format(time.RFC3339))
			return
		default:
			failRecordingXBroadcast(ctx, rec.ID, fmt.Errorf("unknown X broadcast action %q", action))
			return
		}

		next, nextAction, err := getters.ClaimRecordingXBroadcastStep(ctx, rec.ID, operation.ScheduledAt, operation.SessionID, operation.OptimisticPosterURL, time.Now())
		if err != nil {
			failRecordingXBroadcast(ctx, rec.ID, err)
			return
		}
		if nextAction == "" {
			failRecordingXBroadcast(ctx, rec.ID, fmt.Errorf("another worker claimed the next X broadcast step"))
			return
		}
		operation, action = next, nextAction
	}
}

func failRecordingXBroadcast(ctx *config.AppContext, recordingID string, err error) {
	message := err.Error()
	setXJobStatus(recordingID, "failed", message)
	if saveErr := getters.FailRecordingXBroadcast(ctx, recordingID, message, time.Now()); saveErr != nil {
		ctx.Err.Printf("recording X broadcast failure recording=%s error=%s persist=%s", recordingID, message, saveErr)
		return
	}
	ctx.Err.Printf("recording X broadcast failed recording=%s: %s", recordingID, message)
}

func recordingBroadcastTitle(row *RecordingRow) string {
	if row != nil && row.ConfTalk != nil && row.ConfTalk.Proposal != nil && strings.TrimSpace(row.ConfTalk.Proposal.Title) != "" {
		return strings.TrimSpace(row.ConfTalk.Proposal.Title)
	}
	if row != nil && row.Recording != nil {
		return strings.TrimSpace(row.Recording.TalkName)
	}
	return "Bitcoin++ broadcast"
}
