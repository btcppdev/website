package getters

import (
	"testing"
	"time"
)

func TestDatabaseSmokeConferenceBroadcastLifecycle(t *testing.T) {
	ctx := databaseSmokeContext(t)
	id, _ := insertSmokeConference(t, ctx)
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE conferences SET publication_status = 'published' WHERE id = $1::uuid`, id); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	update := ConferenceBroadcastUpdate{Title: "Day 3", State: "live", HLSURL: "https://stream.example/live.m3u8", Now: now}
	live, err := UpsertConferenceBroadcast(ctx, id, update)
	if err != nil {
		t.Fatal(err)
	}
	if live.Title != "Day 3" || live.StartedAt == nil || !live.StartedAt.Equal(now) || live.HeartbeatAt == nil {
		t.Fatalf("live=%+v", live)
	}
	active, err := GetActiveConferenceBroadcast(ctx, now.Add(-time.Minute))
	if err != nil || active == nil || active.ConferenceID != id {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	stale, err := GetActiveConferenceBroadcast(ctx, now.Add(time.Second))
	if err != nil || stale != nil {
		t.Fatalf("stale=%+v err=%v", stale, err)
	}
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE conferences SET publication_status = 'draft' WHERE id = $1::uuid`, id); err != nil {
		t.Fatal(err)
	}
	active, err = GetActiveConferenceBroadcast(ctx, now.Add(-time.Minute))
	if err != nil || active != nil {
		t.Fatalf("draft is public: %+v err=%v", active, err)
	}
	update.Now = now.Add(45 * time.Second)
	live, err = UpsertConferenceBroadcast(ctx, id, update)
	if err != nil || live == nil || !live.StartedAt.Equal(now) || !live.HeartbeatAt.Equal(update.Now) {
		t.Fatalf("heartbeat=%+v err=%v", live, err)
	}
	update.State = "ended"
	ended, err := UpsertConferenceBroadcast(ctx, id, update)
	if err != nil || ended == nil || ended.EndedAt == nil {
		t.Fatalf("ended=%+v err=%v", ended, err)
	}
	update.State = "live"
	update.Now = now.Add(time.Hour)
	restarted, err := UpsertConferenceBroadcast(ctx, id, update)
	if err != nil || restarted == nil || !restarted.StartedAt.Equal(update.Now) || restarted.EndedAt != nil {
		t.Fatalf("restarted=%+v err=%v", restarted, err)
	}
}
