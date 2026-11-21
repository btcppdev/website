package publishing

import (
	"context"
	"errors"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
)

func cancelItem(t *testing.T, s Store, id string) RecordingItem {
	t.Helper()
	d := itemFor(t, s, id)
	if !d.Cancelable {
		t.Fatalf("not cancelable: %+v", d)
	}
	if err := s.SaveRecordingMoves(context.Background(), []RecordingMove{{ID: id, Revision: d.Revision}}); err != nil {
		t.Fatal(err)
	}
	return itemFor(t, s, id)
}

func TestBroadcastCancellationLifecycle(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	ctx := context.Background()
	id, at := recordingFixture(t, s)
	seedBroadcast(t, s, id, at)
	if _, err := s.DB.Exec(ctx, `UPDATE recordings SET publish_at=$2 WHERE id=$1`, id, at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	before := itemFor(t, s, "broadcast:"+id)
	pending := cancelItem(t, s, before.ID)
	if pending.State != "pending" || !pending.CancelPending || pending.Eligible || pending.Cancelable || !pending.At.Equal(at) {
		t.Fatalf("pending cancellation: %+v", pending)
	}
	if err := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: before.ID, Revision: before.Revision}}); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale cancellation: %v", err)
	}
	calendar, err := s.Calendar(ctx, at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range calendar {
		if e.ID == before.ID && e.At != nil && e.At.Equal(at) {
			found = true
		}
	}
	if !found {
		t.Fatal("pending cancellation disappeared from calendar")
	}
	app := &config.AppContext{DB: s.DB}
	plans, err := getters.ListRecordingBroadcastPlans(app, getters.RecordingBroadcastPlanFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, p := range plans {
		if p.RecordingID == id {
			found = true
			if p.Status != "canceled" || !p.ScheduledAt.Equal(at) {
				t.Fatalf("playback not disabled: %+v", p)
			}
		}
	}
	if !found {
		t.Fatal("missing streamer cancellation tombstone")
	}
	reservations, err := getters.RecordingScheduleReservations(ctx, s.DB)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, r := range reservations {
		if r.RecordingID == id && r.PublishAt.Equal(at) {
			found = true
		}
	}
	if !found {
		t.Fatal("released slot before confirmation")
	}
	worker := RecordingScheduler{Store: s, Apply: func(_ context.Context, d RecordingChange) error {
		if d.ChannelID != "broadcasts" || d.TargetAt != nil || d.BroadcastID != before.BroadcastID {
			t.Fatalf("wrong provider change: %+v", d)
		}
		return nil
	}}
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	after := itemFor(t, s, before.ID)
	if after.State != "draft" || after.At != nil || after.ConfirmedAt != nil || after.CancelPending || !after.Eligible {
		t.Fatalf("not returned to unscheduled: %+v", after)
	}
	if yt := itemFor(t, s, "youtube:"+id); yt.At == nil || !yt.At.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("YouTube was changed: %+v", yt)
	}
	reservations, err = getters.RecordingScheduleReservations(ctx, s.DB)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reservations {
		if r.RecordingID == id && r.PublishAt.Equal(at) {
			t.Fatal("confirmed cancellation still reserves slot")
		}
	}
	// A canceled provider broadcast cannot be reused by a later schedule.
	next := at.Add(4 * time.Hour)
	if err = s.SaveRecordingMoves(ctx, []RecordingMove{{ID: after.ID, Revision: after.Revision, At: &next}}); err != nil {
		t.Fatal(err)
	}
	operation, action, err := getters.ClaimRecordingXBroadcastStep(app, id, next, "new-session", "new-poster", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if action != "create" || operation.BroadcastID != "" || operation.PosterMediaID != "" || operation.SessionID != "new-session" {
		t.Fatalf("reused deleted broadcast: %s %+v", action, operation)
	}
}

func TestCancellationUncertainResultIsReadOnlyReconciled(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id, at := recordingFixture(t, s)
	seedBroadcast(t, s, id, at)
	cancelItem(t, s, "broadcast:"+id)
	writes, reads := 0, 0
	w := RecordingScheduler{Store: s, Apply: func(context.Context, RecordingChange) error { writes++; return errors.New("provider timed out") }, Confirm: func(_ context.Context, d RecordingChange) (bool, error) {
		reads++
		return d.TargetAt == nil && d.BroadcastID != "", nil
	}}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	d := itemFor(t, s, "broadcast:"+id)
	if d.State != "needs_review" || !d.CancelPending || d.At == nil {
		t.Fatalf("lost uncertain cancellation: %+v", d)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE recording_schedule_changes SET updated_at=now()-interval '10 minutes' WHERE recording_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || reads != 1 || itemFor(t, s, d.ID).State != "draft" {
		t.Fatalf("writes=%d reads=%d item=%+v", writes, reads, itemFor(t, s, d.ID))
	}
}

func TestCancelQueuedBroadcastBeforeProviderCreation(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	ctx := context.Background()
	id, at := recordingFixture(t, s)
	d := itemFor(t, s, "broadcast:"+id)
	if err := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &at}}); err != nil {
		t.Fatal(err)
	}
	cancelItem(t, s, d.ID)
	w := RecordingScheduler{Store: s, Apply: func(_ context.Context, d RecordingChange) error {
		if d.BroadcastID != "" || d.TargetAt != nil {
			t.Fatalf("unexpected create: %+v", d)
		}
		return nil
	}}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := itemFor(t, s, d.ID); got.State != "draft" || got.At != nil {
		t.Fatalf("queue not canceled: %+v", got)
	}
}

func TestCancellationDoesNotNeedMediaOrRuntime(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id, at := recordingFixture(t, s)
	seedBroadcast(t, s, id, at)
	if _, err := s.DB.Exec(ctx, `UPDATE recordings SET duration_seconds=0,file_uri='' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	cancelItem(t, s, "broadcast:"+id)
	called := false
	w := RecordingScheduler{Store: s, Apply: func(context.Context, RecordingChange) error { called = true; return nil }}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("missing media blocked cancellation")
	}
}

func TestCancellationRejectsUnsafeStates(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	base := RecordingItem{ChannelID: "broadcasts", BroadcastID: "id", At: &future, ConfirmedAt: &future, State: "scheduled", SourceState: "scheduled"}
	for _, state := range []string{"live", "ended", "published", "syncing", "needs_review", "creating", "finalizing"} {
		d := base
		d.State = state
		if recordingCancellationUnavailable(d, now) == "" {
			t.Errorf("allowed %s", state)
		}
	}
	d := base
	soon := now.Add(30 * time.Second)
	d.ConfirmedAt = &soon
	if recordingCancellationUnavailable(d, now) == "" {
		t.Fatal("allowed imminent cancellation")
	}
	d = base
	d.BroadcastID = ""
	d.SourceState = "failed"
	if recordingCancellationUnavailable(d, now) == "" {
		t.Fatal("allowed unidentified remote broadcast")
	}
	if validateRecordingCancellation(base, RecordingChange{BroadcastID: "replacement"}, now) == "" {
		t.Fatal("allowed replacement broadcast")
	}
}

func TestCancellationConfirmationCannotAffectReplacementBroadcast(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id, at := recordingFixture(t, s)
	seedBroadcast(t, s, id, at)
	cancelItem(t, s, "broadcast:"+id)
	if _, err := s.DB.Exec(ctx, `UPDATE recording_schedule_changes SET state='needs_review',updated_at=now()-interval '10 minutes' WHERE recording_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	w := RecordingScheduler{Store: s, Confirm: func(context.Context, RecordingChange) (bool, error) {
		if _, err := s.DB.Exec(ctx, `UPDATE recording_x_broadcasts SET broadcast_id='replacement',scheduled_broadcast_id='replacement' WHERE recording_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		return true, nil
	}}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	d := itemFor(t, s, "broadcast:"+id)
	if d.State != "needs_review" || d.SourceState != "scheduled" || d.BroadcastID != "replacement" {
		t.Fatalf("canceled replacement broadcast: %+v", d)
	}
}
