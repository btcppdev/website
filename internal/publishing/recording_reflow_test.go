package publishing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
)

func seedRelease(t *testing.T, s Store, id string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.DB.Exec(ctx, `UPDATE recordings SET publish_at=$2 WHERE id=$1`, id, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO social_posts(ref,posted_to,status,scheduled_at) VALUES('recording:'||$1||':youtube','youtube','scheduled',$2)`, id, at); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Exec(ctx, `DELETE FROM social_posts WHERE ref='recording:'||$1||':youtube'`, id) })
}

func TestRecordingReflowReusesSelectedSlots(t *testing.T) {
	for _, channel := range []string{"youtube", "broadcasts"} {
		t.Run(channel, func(t *testing.T) {
			s := testStore(t)
			recordingSlotsFixture(t, s)
			ctx := context.Background()
			a, _ := recordingFixture(t, s)
			b, _ := recordingFixture(t, s)
			ids := []string{recordingDestinationID(a, channel), recordingDestinationID(b, channel)}
			initial, err := s.PreviewRecordingQueue(ctx, ids)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range initial {
				d := itemFor(t, s, m.ID)
				if channel == "youtube" {
					seedRelease(t, s, d.RecordingID, *m.At)
				} else {
					seedBroadcast(t, s, d.RecordingID, *m.At)
				}
			}
			// Unchanged order can reuse both confirmed times without provider calls.
			preview, err := s.PreviewRecordingQueue(ctx, ids)
			if err != nil {
				t.Fatal(err)
			}
			for i := range preview {
				if !preview[i].At.Equal(*initial[i].At) {
					t.Fatalf("current slot skipped: %v -> %v", initial[i].At, preview[i].At)
				}
			}
			if err = s.SaveRecordingMoves(ctx, preview); err != nil {
				t.Fatal(err)
			}
			var count int
			if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM recording_schedule_changes WHERE recording_id IN ($1,$2)`, a, b).Scan(&count); err != nil || count != 0 {
				t.Fatalf("no-op queued %d changes: %v", count, err)
			}
			// Reordering takes the same two slots, instead of skipping old bookings.
			preview, err = s.PreviewRecordingQueue(ctx, []string{ids[1], ids[0]})
			if err != nil {
				t.Fatal(err)
			}
			for i := range preview {
				if !preview[i].At.Equal(*initial[i].At) {
					t.Fatalf("selected slot unavailable: %v -> %v", initial[i].At, preview[i].At)
				}
			}
			if err = s.SaveRecordingMoves(ctx, preview); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecordingReflowPreservesOtherDestinations(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	ctx := context.Background()
	a, at := recordingFixture(t, s)
	b, _ := recordingFixture(t, s)
	seedRelease(t, s, a, at)
	seedBroadcast(t, s, a, at)
	first, second := itemFor(t, s, "youtube:"+a), itemFor(t, s, "youtube:"+b)
	next := at.Add(2 * time.Hour)
	// Moving only A's YouTube release cannot free A's still-scheduled X stream.
	err := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: first.ID, Revision: first.Revision, At: &next}, {ID: second.ID, Revision: second.Revision, At: &at}})
	if err == nil {
		t.Fatal("released unselected broadcast reservation")
	}
	if got := itemFor(t, s, first.ID); !got.At.Equal(at) || got.State != "scheduled" {
		t.Fatalf("partial batch: %+v", got)
	}
	// Once both destinations are selected, their old shared time can be reused.
	x := itemFor(t, s, "broadcast:"+a)
	moves := []RecordingMove{{ID: first.ID, Revision: first.Revision, At: &next}, {ID: x.ID, Revision: x.Revision, At: &next}, {ID: second.ID, Revision: second.Revision, At: &at}}
	if err = s.SaveRecordingMoves(ctx, moves); err != nil {
		t.Fatal(err)
	}
	// All old and new times stay reserved outside the batch until confirmation.
	c, _ := recordingFixture(t, s)
	other := itemFor(t, s, "youtube:"+c)
	for _, slot := range []time.Time{at, next} {
		if err = s.SaveRecordingMoves(ctx, []RecordingMove{{ID: other.ID, Revision: other.Revision, At: &slot}}); err == nil {
			t.Fatalf("pending reservation released: %v", slot)
		}
	}
}

// Simulate confirmed provider updates, checking the same reservations used by
// X's real scheduling integration at every intermediate step.
func reflowApply(t *testing.T, s Store, calls *[]RecordingChange) func(context.Context, RecordingChange) error {
	t.Helper()
	return func(ctx context.Context, d RecordingChange) error {
		*calls = append(*calls, d)
		item := itemFor(t, s, recordingDestinationID(d.RecordingID, d.ChannelID))
		reserved, err := getters.RecordingScheduleReservations(ctx, s.DB)
		if err != nil {
			t.Fatal(err)
		}
		conflict, err := getters.RecordingScheduleConflictForRecording(d.RecordingID, *d.TargetAt, item.DurationSeconds, reserved)
		if err != nil || conflict != nil {
			t.Fatalf("provider move overlaps: %+v %v", conflict, err)
		}
		if d.ChannelID == "broadcasts" {
			// Exercise the integration's second validation too, not just our worker.
			_, _, err = getters.ClaimRecordingXBroadcastStep(&config.AppContext{DB: s.DB}, d.RecordingID, *d.TargetAt, "session", "poster", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.DB.Exec(ctx, `UPDATE recording_x_broadcasts SET status='scheduled',scheduled_at=$2 WHERE recording_id=$1`, d.RecordingID, d.TargetAt)
		}
		return err
	}
}

func TestRecordingReflowAppliesDependenciesBeforeReusingSlots(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	ctx := context.Background()
	a, at := recordingFixture(t, s)
	b, _ := recordingFixture(t, s)
	seedRelease(t, s, a, at)
	seedBroadcast(t, s, a, at)
	next := at.Add(2 * time.Hour)
	moves := []RecordingMove{}
	// Request the dependent move first to prove worker order is dependency-based.
	for _, spec := range []struct {
		id string
		at time.Time
	}{{"youtube:" + b, at}, {"youtube:" + a, next}, {"broadcast:" + a, next}} {
		d := itemFor(t, s, spec.id)
		moves = append(moves, RecordingMove{ID: d.ID, Revision: d.Revision, At: &spec.at})
	}
	if err := s.SaveRecordingMoves(ctx, moves); err != nil {
		t.Fatal(err)
	}
	var calls []RecordingChange
	w := RecordingScheduler{Store: s, Apply: reflowApply(t, s, &calls)}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[2].RecordingID != b {
		t.Fatalf("wrong provider order: %+v", calls)
	}
	for _, d := range calls {
		if d.Staging {
			t.Fatal("unnecessary temporary move")
		}
	}
	for _, m := range moves {
		d := itemFor(t, s, m.ID)
		if d.State != "scheduled" || !d.ConfirmedAt.Equal(*m.At) {
			t.Fatalf("unfinished move: %+v", d)
		}
	}
}

func TestRecordingReflowSwapsSafelyAndRecovers(t *testing.T) {
	for _, channel := range []string{"youtube", "broadcasts"} {
		for _, failure := range []string{"none", "rejected", "uncertain"} {
			t.Run(channel+"/"+failure, func(t *testing.T) {
				s := testStore(t)
				recordingSlotsFixture(t, s)
				ctx := context.Background()
				a, at := recordingFixture(t, s)
				b, _ := recordingFixture(t, s)
				next := at.Add(2 * time.Hour)
				if channel == "youtube" {
					seedRelease(t, s, a, at)
					seedRelease(t, s, b, next)
				} else {
					seedBroadcast(t, s, a, at)
					seedBroadcast(t, s, b, next)
				}
				first, second := itemFor(t, s, recordingDestinationID(a, channel)), itemFor(t, s, recordingDestinationID(b, channel))
				moves := []RecordingMove{{ID: first.ID, Revision: first.Revision, At: &next}, {ID: second.ID, Revision: second.Revision, At: &at}}
				if err := s.SaveRecordingMoves(ctx, moves); err != nil {
					t.Fatal(err)
				}
				var staging time.Time
				if err := s.DB.QueryRow(ctx, `SELECT staging_at FROM recording_schedule_changes WHERE recording_id=$1 AND channel_id=$2`, a, channel).Scan(&staging); err != nil {
					t.Fatal(err)
				}
				var calls []RecordingChange
				apply := reflowApply(t, s, &calls)
				w := RecordingScheduler{Store: s, Apply: func(ctx context.Context, d RecordingChange) error {
					if failure != "none" {
						calls = append(calls, d)
						if failure == "rejected" {
							return &ScheduleApplyError{Message: "provider rejected move"}
						}
						return errors.New("provider result lost")
					}
					return apply(ctx, d)
				}}
				if err := w.RunOnce(ctx); err != nil {
					t.Fatal(err)
				}
				if failure != "none" {
					if len(calls) != 1 || !calls[0].Staging || !calls[0].TargetAt.Equal(staging) {
						t.Fatalf("unsafe/repeated move: %+v", calls)
					}
					blocked := itemFor(t, s, second.ID)
					if blocked.State != "pending" || !strings.Contains(blocked.Error, "Waiting for") || !blocked.ConfirmedAt.Equal(next) {
						t.Fatalf("dependent move did not wait: %+v", blocked)
					}
					// An unrelated recording cannot steal old, final, or temporary slots.
					c, _ := recordingFixture(t, s)
					other := itemFor(t, s, recordingDestinationID(c, channel))
					for _, slot := range []time.Time{at, next, staging} {
						if err := s.SaveRecordingMoves(ctx, []RecordingMove{{ID: other.ID, Revision: other.Revision, At: &slot}}); err == nil {
							t.Fatalf("lost reservation at %v", slot)
						}
					}
					failed := itemFor(t, s, first.ID)
					if err := s.RetryRecordingChange(ctx, failed.ID, failed.Revision, false); err != nil {
						t.Fatal(err)
					}
					w.Apply = apply
					if err := w.RunOnce(ctx); err != nil {
						t.Fatal(err)
					}
				}
				want := 3
				if failure != "none" {
					want++
				}
				if len(calls) != want {
					t.Fatalf("calls=%d want=%d: %+v", len(calls), want, calls)
				}
				for _, m := range moves {
					d := itemFor(t, s, m.ID)
					if d.State != "scheduled" || !d.ConfirmedAt.Equal(*m.At) {
						t.Fatalf("swap unfinished: %+v", d)
					}
				}
				reserved, err := getters.RecordingScheduleReservations(ctx, s.DB)
				if err != nil {
					t.Fatal(err)
				}
				for _, r := range reserved {
					if r.PublishAt.Equal(staging) {
						t.Fatal("temporary reservation was not freed")
					}
				}
			})
		}
	}
}

func TestRecordingReflowReconcilesTemporaryYouTubeMove(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	ctx := context.Background()
	a, at := recordingFixture(t, s)
	b, _ := recordingFixture(t, s)
	next := at.Add(2 * time.Hour)
	seedRelease(t, s, a, at)
	seedRelease(t, s, b, next)
	first, second := itemFor(t, s, "youtube:"+a), itemFor(t, s, "youtube:"+b)
	moves := []RecordingMove{{ID: first.ID, Revision: first.Revision, At: &next}, {ID: second.ID, Revision: second.Revision, At: &at}}
	if err := s.SaveRecordingMoves(ctx, moves); err != nil {
		t.Fatal(err)
	}
	var remote RecordingChange
	w := RecordingScheduler{Store: s, Apply: func(_ context.Context, d RecordingChange) error {
		if !d.Staging {
			t.Fatal("dependent update sent before confirmation")
		}
		remote = d // Provider moved it, but the response was lost.
		return errors.New("response lost")
	}}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if remote.TargetAt == nil {
		t.Fatal("temporary move not sent")
	}
	if _, err := s.DB.Exec(ctx, `UPDATE recording_schedule_changes SET updated_at=now()-interval '10 minutes' WHERE recording_id=$1`, a); err != nil {
		t.Fatal(err)
	}
	var calls []RecordingChange
	checks := 0
	w.Confirm = func(_ context.Context, d RecordingChange) (bool, error) {
		checks++
		if !d.Staging || !d.TargetAt.Equal(*remote.TargetAt) || !d.FinalAt.Equal(next) {
			t.Fatalf("checked wrong provider time: %+v", d)
		}
		return true, nil
	}
	w.Apply = reflowApply(t, s, &calls)
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if checks != 1 || len(calls) != 2 {
		t.Fatalf("checks=%d writes=%d", checks, len(calls))
	}
	for _, d := range calls {
		if d.Staging {
			t.Fatal("temporary provider write was repeated")
		}
	}
	for _, m := range moves {
		d := itemFor(t, s, m.ID)
		if d.State != "scheduled" || !d.ConfirmedAt.Equal(*m.At) {
			t.Fatalf("unfinished recovery: %+v", d)
		}
	}
}

func TestRecordingReflowSwapsBothDestinations(t *testing.T) {
	s := testStore(t)
	recordingSlotsFixture(t, s)
	ctx := context.Background()
	a, at := recordingFixture(t, s)
	b, _ := recordingFixture(t, s)
	next := at.Add(2 * time.Hour)
	seedRelease(t, s, a, at)
	seedBroadcast(t, s, a, at)
	seedRelease(t, s, b, next)
	seedBroadcast(t, s, b, next)
	var moves []RecordingMove
	for _, spec := range []struct {
		id string
		at time.Time
	}{{"youtube:" + a, next}, {"broadcast:" + a, next}, {"youtube:" + b, at}, {"broadcast:" + b, at}} {
		d := itemFor(t, s, spec.id)
		moves = append(moves, RecordingMove{ID: d.ID, Revision: d.Revision, At: &spec.at})
	}
	if err := s.SaveRecordingMoves(ctx, moves); err != nil {
		t.Fatal(err)
	}
	var calls []RecordingChange
	w := RecordingScheduler{Store: s, Apply: reflowApply(t, s, &calls)}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 6 {
		t.Fatalf("expected two temporary and four final moves, got %+v", calls)
	}
	for _, m := range moves {
		d := itemFor(t, s, m.ID)
		if d.State != "scheduled" || !d.ConfirmedAt.Equal(*m.At) {
			t.Fatalf("unfinished paired swap: %+v", d)
		}
	}
}
