package publishing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func recordingFixture(t *testing.T, s Store) (string, time.Time) {
	t.Helper()
	ctx := context.Background()
	conf := uuid.NewString()
	var id string
	_, e := s.DB.Exec(ctx, `INSERT INTO conferences(id,tag) VALUES($1,$2)`, conf, "publishing-"+conf)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(ctx, `DELETE FROM conferences WHERE id=$1`, conf) })
	e = s.DB.QueryRow(ctx, `WITH p AS(INSERT INTO proposals(conference_id,title) VALUES($1,'Scheduler fixture') RETURNING id),t AS(INSERT INTO conf_talks(conference_id,proposal_id) SELECT $1,id FROM p RETURNING id) INSERT INTO recordings(conf_talk_id,talk_name,youtube_url,file_uri,duration_seconds) SELECT id,'Scheduler fixture','https://youtu.be/abcdefghijk','recordings/fixture.mp4',3600 FROM t RETURNING id::text`, conf).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().AddDate(0, 0, 3)
	at := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
	return id, at
}
func recordingSlotsFixture(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	// An isolated DB is required; restore channel settings after each test.
	var zone string
	var oldSlots []Slot
	if e := s.DB.QueryRow(ctx, `SELECT timezone,slots FROM publishing_channels WHERE id='broadcasts'`).Scan(&zone, &oldSlots); e != nil {
		t.Fatal(e)
	}
	var slots []Slot
	for day := 0; day < 7; day++ {
		for _, hour := range []string{"12:00", "14:00", "16:00"} {
			slots = append(slots, Slot{Day: day, Time: hour})
		}
	}
	if e := s.SaveBroadcastSlots(ctx, Channel{ID: "broadcasts", Timezone: "UTC", Slots: slots}); e != nil {
		t.Fatal(e)
	}
	// Existing YouTube slots are retained; fixtures add a distinct timezone/cadence.
	var inserted []string
	for _, sl := range slots {
		var id string
		e := s.DB.QueryRow(ctx, `INSERT INTO youtube_publish_slots(channel,weekday,publish_time,timezone,active) VALUES('btcpp-youtube',$1,$2::time,'UTC',true) ON CONFLICT DO NOTHING RETURNING id::text`, sl.Day, sl.Time).Scan(&id)
		if e == nil {
			inserted = append(inserted, id)
		}
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(ctx, `DELETE FROM youtube_publish_slots WHERE id=ANY($1::uuid[])`, inserted)
		_ = s.SaveBroadcastSlots(ctx, Channel{ID: "broadcasts", Timezone: zone, Slots: oldSlots})
	})
}
func itemFor(t *testing.T, s Store, id string) RecordingItem {
	t.Helper()
	items, e := s.RecordingItems(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	d, e := recordingByID(items, id)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func seedBroadcast(t *testing.T, s Store, id string, at time.Time) {
	t.Helper()
	_, e := s.DB.Exec(context.Background(), `INSERT INTO recording_x_broadcasts(recording_id,status,scheduled_at,scheduled_broadcast_id,broadcast_id,poster_media_id,poster_url,x_session_id,optimistic_poster_url) VALUES($1,'scheduled',$2,$3,$3,'poster','https://example.test/poster','session','poster')`, id, at, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
}
func TestRecordingDestinationsMoveIndependently(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	if _, e := s.DB.Exec(ctx, `UPDATE recordings SET publish_at=$2 WHERE id=$1`, id, at); e != nil {
		t.Fatal(e)
	}
	broadcastAt := at.Add(2 * time.Hour)
	seedBroadcast(t, s, id, broadcastAt)
	yt := itemFor(t, s, "youtube:"+id)
	next := at.Add(4 * time.Hour)
	if e := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: yt.ID, Revision: yt.Revision, At: &next}}); e != nil {
		t.Fatal(e)
	}
	pending := itemFor(t, s, yt.ID)
	if pending.State != "pending" || !pending.At.Equal(next) || !pending.ConfirmedAt.Equal(at) {
		t.Fatalf("pending=%+v", pending)
	}
	if e := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: yt.ID, Revision: yt.Revision, At: &next}}); !errors.Is(e, ErrChanged) {
		t.Fatalf("stale edit=%v", e)
	}
	xb := itemFor(t, s, "broadcast:"+id)
	if !xb.At.Equal(broadcastAt) {
		t.Fatal("YouTube move changed broadcast")
	}
	worker := RecordingScheduler{Store: s, Apply: func(_ context.Context, d RecordingChange) error {
		if d.ChannelID != "youtube" || !d.TargetAt.Equal(next) {
			t.Fatalf("unexpected sync %+v", d)
		}
		return nil
	}}
	if e := worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	done := itemFor(t, s, yt.ID)
	if done.State != "scheduled" || !done.ConfirmedAt.Equal(next) {
		t.Fatalf("confirmed=%+v", done)
	}
	xb = itemFor(t, s, xb.ID)
	if !xb.At.Equal(broadcastAt) {
		t.Fatal("synchronization changed other destination")
	}
	moveBroadcast := at.AddDate(0, 0, 1)
	if e := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: xb.ID, Revision: xb.Revision, At: &moveBroadcast}}); e != nil {
		t.Fatal(e)
	}
	if got := itemFor(t, s, yt.ID); !got.At.Equal(next) {
		t.Fatal("broadcast move changed YouTube")
	}
}
func TestRecordingReservationsSurviveProviderFailure(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	seedBroadcast(t, s, id, at)
	d := itemFor(t, s, "broadcast:"+id)
	next := at.Add(4 * time.Hour)
	if e := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &next}}); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	worker := RecordingScheduler{Store: s, Apply: func(_ context.Context, change RecordingChange) error {
		calls.Add(1)
		_, e := s.DB.Exec(ctx, `UPDATE recording_x_broadcasts SET scheduled_at=$2,status='failed' WHERE recording_id=$1`, id, next)
		if e != nil {
			t.Fatal(e)
		}
		return fmt.Errorf("provider result lost")
	}}
	if e := worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if e := worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("uncertain provider call repeated")
	}
	failed := itemFor(t, s, d.ID)
	if failed.State != "needs_review" || !failed.ConfirmedAt.Equal(at) {
		t.Fatalf("lost confirmed reservation: %+v", failed)
	}
	other, _ := recordingFixture(t, s)
	o := itemFor(t, s, "broadcast:"+other)
	for _, slot := range []time.Time{at, next} {
		if e := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: o.ID, Revision: o.Revision, At: &slot}}); e == nil {
			t.Fatalf("allowed conflict at %v", slot)
		}
	}
	if e := s.RetryRecordingChange(ctx, failed.ID, failed.Revision, false); e != nil {
		t.Fatal(e)
	} // Stable broadcast ID makes retry resumable.
}
func TestRecordingBatchIsAtomicAndUsesRuntime(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	ctx := context.Background()
	a, at := recordingFixture(t, s)
	b, _ := recordingFixture(t, s)
	if _, e := s.DB.Exec(ctx, `UPDATE recordings SET duration_seconds=7200 WHERE id=$1`, a); e != nil {
		t.Fatal(e)
	}
	first := itemFor(t, s, "broadcast:"+a)
	second := itemFor(t, s, "broadcast:"+b)
	overlap := at.Add(2 * time.Hour)
	if e := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: first.ID, Revision: first.Revision, At: &at}, {ID: second.ID, Revision: second.Revision, At: &overlap}}); e == nil {
		t.Fatal("allowed runtime+gap overlap")
	}
	if got := itemFor(t, s, first.ID); got.At != nil {
		t.Fatal("partial batch committed")
	}
	preview, e := s.PreviewRecordingQueue(ctx, []string{first.ID, second.ID})
	if e != nil {
		t.Fatal(e)
	}
	if len(preview) != 2 || preview[1].At.Sub(*preview[0].At) < 2*time.Hour+5*time.Minute {
		t.Fatalf("preview overlaps: %+v", preview)
	}
	if e = s.SaveRecordingMoves(ctx, preview); e != nil {
		t.Fatal(e)
	}
}
func TestRecordingMovesSerializeAcrossEvents(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	a, at := recordingFixture(t, s)
	b, _ := recordingFixture(t, s)
	first := itemFor(t, s, "broadcast:"+a)
	second := itemFor(t, s, "broadcast:"+b)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, d := range []RecordingItem{first, second} {
		wg.Add(1)
		go func(d RecordingItem) {
			defer wg.Done()
			if s.SaveRecordingMoves(context.Background(), []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &at}}) == nil {
				successes.Add(1)
			}
		}(d)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("slot claimed %d times", successes.Load())
	}
}
func TestRecordingSlotAndLifecycleRestrictions(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	d := itemFor(t, s, "youtube:"+id)
	arbitrary := at.Add(15 * time.Minute)
	past := at.AddDate(0, 0, -5)
	for _, bad := range []time.Time{arbitrary, past} {
		if s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &bad}}) == nil {
			t.Fatal("accepted invalid slot")
		}
	}
	if _, e := s.DB.Exec(ctx, `UPDATE recordings SET duration_seconds=0 WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	d = itemFor(t, s, d.ID)
	if d.Eligible || s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &at}}) == nil {
		t.Fatal("accepted unknown duration")
	}
	if _, e := s.DB.Exec(ctx, `UPDATE recordings SET duration_seconds=3600 WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.Exec(ctx, `INSERT INTO recording_broadcasts(recording_id,state,hls_url,started_at,heartbeat_at) VALUES($1,'live','https://example.test/live',now(),now())`, id); e != nil {
		t.Fatal(e)
	}
	b := itemFor(t, s, "broadcast:"+id)
	if b.Eligible || s.SaveRecordingMoves(ctx, []RecordingMove{{ID: b.ID, Revision: b.Revision, At: &at}}) == nil {
		t.Fatal("moved a live broadcast")
	}
}
func TestRecordingClearWaitsForProviderAndPreservesBroadcast(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	if _, e := s.DB.Exec(ctx, `UPDATE recordings SET publish_at=$2 WHERE id=$1`, id, at); e != nil {
		t.Fatal(e)
	}
	seedBroadcast(t, s, id, at)
	d := itemFor(t, s, "youtube:"+id)
	if e := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision}}); e != nil {
		t.Fatal(e)
	}
	pending := itemFor(t, s, d.ID)
	if pending.At != nil || pending.ConfirmedAt == nil {
		t.Fatal("clear was prematurely confirmed")
	}
	if e := (RecordingScheduler{Store: s, Apply: func(context.Context, RecordingChange) error { return nil }}).RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if itemFor(t, s, d.ID).ConfirmedAt != nil {
		t.Fatal("clear not confirmed")
	}
	if !itemFor(t, s, "broadcast:"+id).At.Equal(at) {
		t.Fatal("clear changed broadcast")
	}
}
func TestRecordingInterruptedSyncRequiresReview(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	d := itemFor(t, s, "broadcast:"+id)
	if e := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &at}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.Exec(ctx, `UPDATE recording_schedule_changes SET state='syncing',lease_until=now()-interval '1 minute' WHERE recording_id=$1`, id); e != nil {
		t.Fatal(e)
	}
	calls := 0
	if e := (RecordingScheduler{Store: s, Apply: func(context.Context, RecordingChange) error { calls++; return nil }}).RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	d = itemFor(t, s, d.ID)
	if calls != 0 || d.State != "needs_review" || !d.RetryConfirmation {
		t.Fatalf("unsafe recovery: %+v calls=%d", d, calls)
	}
	if e := s.RetryRecordingChange(ctx, d.ID, d.Revision, false); e == nil {
		t.Fatal("recreated uncertain broadcast without confirmation")
	}
}

func TestRecordingYouTubeVisibilityFollowsVideoIdentity(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	if _, e := s.DB.Exec(ctx, `INSERT INTO recording_youtube_status(recording_id,youtube_url,privacy_status) VALUES($1,'https://youtu.be/abcdefghijk','public')`, id); e != nil {
		t.Fatal(e)
	}
	d := itemFor(t, s, "youtube:"+id)
	if d.Eligible || s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &at}}) == nil {
		t.Fatal("rescheduled a public video")
	}
	if _, e := s.DB.Exec(ctx, `UPDATE recordings SET youtube_url='https://youtu.be/zyxwvutsrqp' WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.Exec(ctx, `UPDATE recordings SET duration_seconds=3600 WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if d = itemFor(t, s, d.ID); !d.Eligible {
		t.Fatalf("old visibility leaked onto replacement: %+v", d)
	}
}
