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
	"unicode/utf8"

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
	conf, rec, _, ok := scopedRecordingFromRequest(w, r, ctx)
	if !ok {
		return
	}
	redirectRecordingPublishing(w, r, conf.Tag, rec.ID, "broadcasts")
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
	needsPoster := row.XBroadcast == nil || strings.TrimSpace(row.XBroadcast.PosterMediaID) == ""
	if needsPoster {
		if strings.TrimSpace(cardKey) == "" {
			return nil, "", fmt.Errorf("generate a talk social card before scheduling an X broadcast")
		}
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

// Keep the title compact for the broadcast player. This is our display budget,
// not a claim about X's limit. Only the talk title is ellipsized: handles stay whole.
const recordingBroadcastTitleLimit = 140

func recordingBroadcastTitle(row *RecordingRow) string {
	title := "Bitcoin++ broadcast"
	if row == nil {
		return title
	}
	if row.Recording != nil && strings.TrimSpace(row.Recording.TalkName) != "" {
		title = row.Recording.TalkName
	}
	event := ""
	if row.ConfTalk != nil {
		if p := row.ConfTalk.Proposal; p != nil && strings.TrimSpace(p.Title) != "" {
			title = p.Title
		}
		if conf := row.ConfTalk.Conf; conf != nil {
			label := strings.TrimSpace(strings.SplitN(conf.Desc, ",", 2)[0])
			if label == "" {
				label = strings.TrimSpace(strings.SplitN(conf.Location, ",", 2)[0])
			}
			if label != "" {
				if strings.HasPrefix(strings.ToLower(label), "bitcoin++") {
					label = strings.TrimSpace(label[len("bitcoin++"):])
				}
				event = " | " + strings.TrimSpace("bitcoin++ "+label)
			}
		}
	}
	title = strings.Join(strings.Fields(title), " ")
	event = strings.Join(strings.Fields(event), " ")
	if event != "" {
		event = " " + event
	}
	var credits []string
	seen := map[string]bool{}
	for _, speaker := range row.Speakers {
		if speaker == nil || speaker.IsDeletedAccount {
			continue
		}
		credit := strings.Join(strings.Fields(speaker.Name), " ")
		if handle := recordingBroadcastHandle(speaker.Twitter.Handle); handle != "" {
			credit = "@" + handle
		}
		if credit == "" || seen[strings.ToLower(credit)] {
			continue
		}
		seen[strings.ToLower(credit)] = true
		credits = append(credits, credit)
	}
	suffix := func(n int) string {
		if n == 0 {
			return ""
		}
		out := " — " + strings.Join(credits[:n], ", ")
		if n < len(credits) {
			out += fmt.Sprintf(" +%d more", len(credits)-n)
		}
		return out
	}
	n := len(credits)
	if utf8.RuneCountInString(title+suffix(n)+event) > recordingBroadcastTitleLimit {
		event = ""
	}
	// Leave at least 40 characters for the talk when a large panel is credited.
	for n > 0 && utf8.RuneCountInString(suffix(n)+event) > recordingBroadcastTitleLimit-40 {
		n--
	}
	ending := suffix(n) + event
	return truncateRunes(title, recordingBroadcastTitleLimit-utf8.RuneCountInString(ending)) + ending
}

func recordingBroadcastHandle(raw string) string {
	handle := types.ParseTwitter(raw).Handle
	if len(handle) == 0 || len(handle) > 15 {
		return ""
	}
	for _, ch := range handle {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_') {
			return ""
		}
	}
	return handle
}
