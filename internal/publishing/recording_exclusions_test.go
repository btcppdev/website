package publishing

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRecordingExclusionPersistsAndBlocksStaleBatches(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, _ := recordingFixture(t, s)
	ctx := context.Background()
	if _, err := s.DB.Exec(ctx, `INSERT INTO recording_youtube_status(recording_id,youtube_url,privacy_status) SELECT id,youtube_url,'private' FROM recordings WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	initial := itemFor(t, s, "youtube:"+id)
	preview, err := s.PreviewRecordingQueue(ctx, []string{initial.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetRecordingExcluded(ctx, initial.ID, initial.Revision, true); err != nil {
		t.Fatal(err)
	}
	excluded := itemFor(t, s, initial.ID)
	if !excluded.Excluded || excluded.Eligible || !strings.Contains(excluded.Reason, "Excluded") {
		t.Fatalf("exclusion was not retained: %+v", excluded)
	}
	if excluded.YouTubePrivacy != "private" {
		t.Fatal("excluding a recording must preserve its private visibility")
	}
	if err = s.SaveRecordingMoves(ctx, preview); err != ErrChanged {
		t.Fatalf("stale preview must fail: %v", err)
	}
	if _, err = s.PreviewRecordingQueue(ctx, []string{initial.ID}); err == nil {
		t.Fatal("excluded item was previewed")
	}
	if _, err = s.AvailableRecordingSlots(ctx, initial.ID, time.Now()); err == nil {
		t.Fatal("excluded item had draggable slots")
	}
	if err = s.SaveRecordingMoves(ctx, []RecordingMove{{ID: initial.ID, Revision: excluded.Revision, At: preview[0].At}}); err == nil {
		t.Fatal("current exclusion was bypassed")
	}
	broadcast := itemFor(t, s, "broadcast:"+id)
	if broadcast.Excluded || !broadcast.Eligible {
		t.Fatal("YouTube exclusion changed the broadcast destination")
	}
	if err = s.SetRecordingExcluded(ctx, initial.ID, initial.Revision, false); err != ErrChanged {
		t.Fatalf("stale restoration must fail: %v", err)
	}
	if err = s.SetRecordingExcluded(ctx, initial.ID, excluded.Revision, false); err != nil {
		t.Fatal(err)
	}
	restored := itemFor(t, s, initial.ID)
	if restored.Excluded || !restored.Eligible || restored.At != nil {
		t.Fatal("restoration should make the item available without scheduling it")
	}
	if err = s.SaveRecordingMoves(ctx, preview); err != ErrChanged {
		t.Fatalf("restoration must not revive a stale preview: %v", err)
	}
}

func TestRecordingExclusionDoesNotPretendToCancelSchedules(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	d := itemFor(t, s, "youtube:"+id)
	if err := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &at}}); err != nil {
		t.Fatal(err)
	}
	d = itemFor(t, s, d.ID)
	if err := s.SetRecordingExcluded(ctx, d.ID, d.Revision, true); err == nil {
		t.Fatal("pending provider work must be resolved before exclusion")
	}
	if _, err := s.DB.Exec(ctx, `UPDATE recording_schedule_changes SET state='applied' WHERE recording_id=$1;`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE recordings SET publish_at=$2 WHERE id=$1`, id, at); err != nil {
		t.Fatal(err)
	}
	d = itemFor(t, s, d.ID)
	if err := s.SetRecordingExcluded(ctx, d.ID, d.Revision, true); err == nil {
		t.Fatal("existing provider schedule must not be hidden by exclusion")
	}
}

func TestRecordingWorkerHonorsExclusion(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	d := itemFor(t, s, "youtube:"+id)
	if err := s.SetRecordingExcluded(ctx, d.ID, d.Revision, true); err != nil {
		t.Fatal(err)
	}
	// Simulate stale work inserted outside the normal scheduling entry points.
	_, err := s.DB.Exec(ctx, `INSERT INTO recording_schedule_changes(recording_id,channel_id,target_at,state,youtube_url,file_uri) VALUES($1,'youtube',$2,'pending',$3,$4)`, id, at, d.YouTubeURL, d.FileURI)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	w := RecordingScheduler{Store: s, Apply: func(context.Context, RecordingChange) error { called = true; return nil }}
	if err = w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("excluded recording reached a provider")
	}
	d = itemFor(t, s, d.ID)
	if d.State != "failed" || !strings.Contains(d.Error, "Excluded") {
		t.Fatalf("excluded work was not stopped: %+v", d)
	}
	if err = s.RetryRecordingChange(ctx, d.ID, d.Revision, false); err == nil {
		t.Fatal("excluded work was retried")
	}
}

func TestRecordingExclusionSerializesWithScheduling(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	id, at := recordingFixture(t, s)
	ctx := context.Background()
	d := itemFor(t, s, "youtube:"+id)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- s.SetRecordingExcluded(ctx, d.ID, d.Revision, true)
	}()
	go func() {
		<-start
		results <- s.SaveRecordingMoves(ctx, []RecordingMove{{ID: d.ID, Revision: d.Revision, At: &at}})
	}()
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("exactly one concurrent operation should succeed, got %d", successes)
	}
	d = itemFor(t, s, d.ID)
	if d.Excluded && d.At != nil {
		t.Fatal("an exclusion succeeded while a schedule was also queued")
	}
}
