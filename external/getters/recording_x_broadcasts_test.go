package getters

import (
	"btcpp-web/internal/types"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestDatabaseSmokeRecordingXBroadcastResumesEachDurableStep(t *testing.T) {
	ctx := databaseSmokeContext(t)
	var recordingID string
	if err := ctx.DB.QueryRow(context.Background(), `SELECT id::text FROM recordings ORDER BY created_at LIMIT 1`).Scan(&recordingID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Skip("database has no recording fixture")
		}
		t.Fatal(err)
	}
	_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM recording_x_broadcasts WHERE recording_id = $1::uuid`, recordingID)
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM recording_x_broadcasts WHERE recording_id = $1::uuid`, recordingID)
	})

	now := time.Now().UTC()
	scheduledAt := now.Add(24 * time.Hour)
	row, action, err := ClaimRecordingXBroadcastStep(ctx, recordingID, scheduledAt, "session-1", "blob:https://studio.x.com/poster-1", now)
	if err != nil || action != "create" || row.Status != "creating" {
		t.Fatalf("initial claim row=%+v action=%q err=%v", row, action, err)
	}
	if err := SaveRecordingXBroadcastCreated(ctx, recordingID, "scheduled-1", "broadcast-1", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	row, action, err = ClaimRecordingXBroadcastStep(ctx, recordingID, scheduledAt, "unused-session", "unused-poster", now.Add(2*time.Second))
	if err != nil || action != "upload_poster" || row.SessionID != "session-1" || row.BroadcastID != "broadcast-1" {
		t.Fatalf("poster claim row=%+v action=%q err=%v", row, action, err)
	}
	if err := SaveRecordingXBroadcastPoster(ctx, recordingID, "poster-media-1", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	row, action, err = ClaimRecordingXBroadcastStep(ctx, recordingID, scheduledAt, "unused-session", "unused-poster", now.Add(4*time.Second))
	if err != nil || action != "finalize" || row.PosterMediaID != "poster-media-1" {
		t.Fatalf("finalize claim row=%+v action=%q err=%v", row, action, err)
	}
	if err := SaveRecordingXBroadcastFinalized(ctx, recordingID, "https://pbs.twimg.com/media/poster.jpg", now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	row, action, err = ClaimRecordingXBroadcastStep(ctx, recordingID, scheduledAt, "unused-session", "unused-poster", now.Add(6*time.Second))
	if err != nil || action != "" || row.Status != "scheduled" || row.PosterURL == "" {
		t.Fatalf("completed row=%+v action=%q err=%v", row, action, err)
	}
	updatedAt := scheduledAt.Add(time.Hour)
	row, action, err = ClaimRecordingXBroadcastStep(ctx, recordingID, updatedAt, "unused-session", "unused-poster", now.Add(7*time.Second))
	if err != nil || action != "finalize" || !row.ScheduledAt.Equal(updatedAt) {
		t.Fatalf("reschedule row=%+v action=%q err=%v", row, action, err)
	}
	if err := SaveRecordingXBroadcastFinalized(ctx, recordingID, "https://pbs.twimg.com/media/poster.jpg", now.Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	listed, err := ListRecordingXBroadcasts(ctx, []string{recordingID})
	if err != nil || listed[recordingID] == nil || listed[recordingID].BroadcastID != "broadcast-1" {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	plans, err := ListRecordingBroadcastPlans(ctx, RecordingBroadcastPlanFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var plan *types.RecordingBroadcastPlan
	for _, candidate := range plans {
		if candidate.RecordingID == recordingID {
			plan = candidate
			break
		}
	}
	if plan == nil || plan.Status != "scheduled" || !plan.ScheduledAt.Equal(updatedAt) || plan.ConferenceTag == "" {
		t.Fatalf("broadcast plan = %+v", plan)
	}
	after := plan.UpdatedAt
	changed, err := ListRecordingBroadcastPlans(ctx, RecordingBroadcastPlanFilter{ConferenceTag: plan.ConferenceTag, UpdatedAfter: &after})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range changed {
		if candidate.RecordingID == recordingID {
			t.Fatalf("exclusive updated_after returned unchanged plan: %+v", candidate)
		}
	}
}
