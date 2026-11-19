package publishing

import (
	"context"
	"testing"
	"time"
)

func uncertainYouTubeFixture(t *testing.T, s Store) (string, time.Time) {
	t.Helper()
	id, at := recordingFixture(t, s)
	_, err := s.DB.Exec(context.Background(), `INSERT INTO recording_schedule_changes(recording_id,channel_id,target_at,state,youtube_url,file_uri,updated_at,error)
 SELECT id,'youtube',$2,'needs_review',youtube_url,file_uri,now()-interval '10 minutes','Uncertain provider result' FROM recordings WHERE id=$1`, id, at)
	if err != nil {
		t.Fatal(err)
	}
	return id, at
}

func TestDelayedYouTubeConfirmationDoesNotRepeatUpdate(t *testing.T) {
	s := testStore(t)
	id, at := uncertainYouTubeFixture(t, s)
	ctx := context.Background()
	_, err := s.DB.Exec(ctx, `INSERT INTO social_posts(ref,posted_to,status) VALUES('recording:'||$1||':youtube','youtube','uploaded')`, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Exec(ctx, `DELETE FROM social_posts WHERE ref='recording:'||$1||':youtube'`, id) })
	calls := 0
	w := RecordingScheduler{Store: s, Confirm: func(_ context.Context, d RecordingChange) (bool, error) {
		calls++
		return d.RecordingID == id && d.TargetAt.Equal(at), nil
	}, Apply: func(context.Context, RecordingChange) error { t.Fatal("repeated provider write"); return nil }}
	if err = w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got := itemFor(t, s, "youtube:"+id)
	if calls != 1 || got.State != "scheduled" || got.ConfirmedAt == nil || !got.ConfirmedAt.Equal(at) {
		t.Fatalf("calls=%d item=%+v", calls, got)
	}
	var status string
	var scheduled time.Time
	if err = s.DB.QueryRow(ctx, `SELECT status,scheduled_at FROM social_posts WHERE ref='recording:'||$1||':youtube'`, id).Scan(&status, &scheduled); err != nil || status != "scheduled" || !scheduled.Equal(at) {
		t.Fatalf("social status=%s at=%v err=%v", status, scheduled, err)
	}
	if err = w.RunOnce(ctx); err != nil || calls != 1 {
		t.Fatalf("completed operation rechecked: %d %v", calls, err)
	}
}

func TestYouTubeRecheckIsRateLimitedAndKeepsReservation(t *testing.T) {
	s := testStore(t)
	id, at := uncertainYouTubeFixture(t, s)
	ctx := context.Background()
	calls := 0
	w := RecordingScheduler{Store: s, Confirm: func(context.Context, RecordingChange) (bool, error) { calls++; return false, nil }}
	for i := 0; i < 2; i++ {
		if err := w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got := itemFor(t, s, "youtube:"+id)
	if calls != 1 || got.State != "needs_review" || !got.At.Equal(at) || got.ConfirmedAt != nil || got.Error == "" {
		t.Fatalf("calls=%d item=%+v", calls, got)
	}
}

func TestYouTubeConfirmationCannotOverwriteConcurrentChange(t *testing.T) {
	for _, change := range []string{"revision", "media"} {
		t.Run(change, func(t *testing.T) {
			s := testStore(t)
			id, at := uncertainYouTubeFixture(t, s)
			ctx := context.Background()
			w := RecordingScheduler{Store: s, Confirm: func(context.Context, RecordingChange) (bool, error) {
				var err error
				if change == "revision" {
					_, err = s.DB.Exec(ctx, `UPDATE recording_schedule_changes SET version=version+1,target_at=$2,updated_at=now() WHERE recording_id=$1`, id, at.Add(time.Hour))
				} else {
					_, err = s.DB.Exec(ctx, `UPDATE recordings SET youtube_url='https://youtu.be/replacement' WHERE id=$1`, id)
				}
				if err != nil {
					t.Fatal(err)
				}
				return true, nil
			}}
			if err := w.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			got := itemFor(t, s, "youtube:"+id)
			if got.ConfirmedAt != nil || got.State != "needs_review" {
				t.Fatalf("stale confirmation accepted: %+v", got)
			}
		})
	}
}

func TestYouTubeClearReconcilesWithoutTouchingBroadcast(t *testing.T) {
	s := testStore(t)
	id, at := uncertainYouTubeFixture(t, s)
	ctx := context.Background()
	seedBroadcast(t, s, id, at)
	if _, err := s.DB.Exec(ctx, `UPDATE recordings SET publish_at=$2 WHERE id=$1`, id, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE recording_schedule_changes SET target_at=NULL,previous_at=$2 WHERE recording_id=$1`, id, at); err != nil {
		t.Fatal(err)
	}
	w := RecordingScheduler{Store: s, Confirm: func(_ context.Context, d RecordingChange) (bool, error) {
		if d.ChannelID != "youtube" || d.TargetAt != nil {
			t.Fatalf("unexpected confirmation: %+v", d)
		}
		return true, nil
	}}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got := itemFor(t, s, "youtube:"+id)
	if got.ConfirmedAt != nil || got.State != "draft" || got.YouTubePrivacy != "unlisted" {
		t.Fatalf("clear not reconciled: %+v", got)
	}
	if broadcast := itemFor(t, s, "broadcast:"+id); broadcast.ConfirmedAt == nil || !broadcast.ConfirmedAt.Equal(at) {
		t.Fatalf("broadcast changed: %+v", broadcast)
	}
}
