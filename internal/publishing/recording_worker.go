package publishing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type RecordingChange struct {
	RecordingID string
	ChannelID   string
	TargetAt    *time.Time
	YouTubeURL  string
	FileURI     string
	Version     int
}

// ScheduleApplyError separates a known rejection from an uncertain remote result.
type ScheduleApplyError struct {
	Message string
	Review  bool
}

func (e *ScheduleApplyError) Error() string { return e.Message }

type RecordingScheduler struct {
	Store Store
	Apply func(context.Context, RecordingChange) error
}

func (w RecordingScheduler) RunOnce(ctx context.Context) error {
	conn, e := w.Store.DB.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var locked bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(7330192404)`).Scan(&locked); e != nil || !locked {
		return e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(7330192404)`); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	if _, e = conn.Exec(ctx, `UPDATE recording_schedule_changes SET state='needs_review',error='Schedule synchronization was interrupted. Check the destination before retrying.',lease_until=NULL,version=version+1,updated_at=now() WHERE state='syncing' AND lease_until<now()`); e != nil {
		return e
	}
	for n := 0; n < 10; n++ {
		tx, e := conn.Begin(ctx)
		if e != nil {
			return e
		}
		if e = lockRecordings(ctx, tx); e != nil {
			tx.Rollback(ctx)
			return e
		}
		var d RecordingChange
		e = tx.QueryRow(ctx, `SELECT recording_id::text,channel_id,target_at,youtube_url,file_uri,version FROM recording_schedule_changes WHERE state='pending' ORDER BY updated_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&d.RecordingID, &d.ChannelID, &d.TargetAt, &d.YouTubeURL, &d.FileURI, &d.Version)
		if errors.Is(e, pgx.ErrNoRows) {
			tx.Rollback(ctx)
			return nil
		}
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		items, e := recordingItems(ctx, tx)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		prefix := "youtube:"
		if d.ChannelID == "broadcasts" {
			prefix = "broadcast:"
		}
		item, e := recordingByID(items, prefix+d.RecordingID)
		reason := ""
		if e != nil {
			reason = e.Error()
		} else if item.Reason != "" {
			reason = item.Reason
		} else if item.YouTubeURL != d.YouTubeURL || item.FileURI != d.FileURI {
			reason = "The recording media changed. Review and schedule the replacement again."
		} else if d.TargetAt != nil && !d.TargetAt.After(time.Now().Add(time.Minute)) {
			reason = "The requested slot is too close or has passed. Choose a future publication slot."
		}
		if reason != "" {
			_, e = tx.Exec(ctx, `UPDATE recording_schedule_changes SET state='failed',error=$3,version=version+1,updated_at=now() WHERE recording_id=$1 AND channel_id=$2`, d.RecordingID, d.ChannelID, reason)
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			if e = tx.Commit(ctx); e != nil {
				return e
			}
			continue
		}
		_, e = tx.Exec(ctx, `UPDATE recording_schedule_changes SET state='syncing',lease_until=now()+interval '5 minutes',version=version+1,updated_at=now() WHERE recording_id=$1 AND channel_id=$2`, d.RecordingID, d.ChannelID)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		d.Version++
		jobctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		if w.Apply == nil {
			e = &ScheduleApplyError{Message: "Recording publisher is not configured"}
		} else {
			e = w.Apply(jobctx, d)
		}
		cancel()
		if e != nil {
			state := "needs_review"
			var known *ScheduleApplyError
			if errors.As(e, &known) && !known.Review {
				state = "failed"
			}
			if _, err := conn.Exec(ctx, `UPDATE recording_schedule_changes SET state=$4,error=$5,lease_until=NULL,version=version+1,updated_at=now() WHERE recording_id=$1 AND channel_id=$2 AND version=$3 AND state='syncing'`, d.RecordingID, d.ChannelID, d.Version, state, e.Error()); err != nil {
				return err
			}
			continue
		}
		// Confirmed source fields and operation completion advance together. A crash
		// before this commit leaves a reviewable operation, never a silent success.
		tx, e = conn.Begin(ctx)
		if e != nil {
			return e
		}
		if e = lockRecordings(ctx, tx); e != nil {
			tx.Rollback(ctx)
			return e
		}
		tag, e := tx.Exec(ctx, `UPDATE recording_schedule_changes SET state='applied',error='',lease_until=NULL,version=version+1,updated_at=now() WHERE recording_id=$1 AND channel_id=$2 AND version=$3 AND state='syncing'`, d.RecordingID, d.ChannelID, d.Version)
		if e != nil || tag.RowsAffected() != 1 {
			tx.Rollback(ctx)
			if e != nil {
				return e
			}
			return ErrChanged
		}
		if d.ChannelID == "youtube" {
			var updated pgconn.CommandTag
			updated, e = tx.Exec(ctx, `UPDATE recordings SET publish_at=$2 WHERE id=$1 AND youtube_url=$3 AND file_uri=$4`, d.RecordingID, d.TargetAt, d.YouTubeURL, d.FileURI)
			if e == nil && updated.RowsAffected() != 1 {
				e = ErrChanged
			}
			if e == nil {
				_, e = tx.Exec(ctx, `UPDATE social_posts SET scheduled_at=$2,status=CASE WHEN $2::timestamptz IS NULL THEN 'canceled' ELSE 'scheduled' END,error='' WHERE ref='recording:'||$1||':youtube'`, d.RecordingID, d.TargetAt)
			}
		}
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	return nil
}
func (s Store) RetryRecordingChange(ctx context.Context, id, revision string, confirmedNotCreated bool) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lockRecordings(ctx, tx); e != nil {
		return e
	}
	items, e := recordingItems(ctx, tx)
	if e != nil {
		return e
	}
	d, e := recordingByID(items, id)
	if e != nil {
		return e
	}
	if d.Revision != revision {
		return ErrChanged
	}
	if d.State != "failed" && d.State != "needs_review" {
		return fmt.Errorf("This schedule change is not awaiting retry")
	}
	if d.ChannelID == "broadcasts" && d.State == "needs_review" && d.BroadcastID == "" && !confirmedNotCreated {
		return fmt.Errorf("Check X Studio first and confirm that no broadcast was created before retrying")
	}
	_, e = tx.Exec(ctx, `UPDATE recording_schedule_changes SET state='pending',error='',lease_until=NULL,version=version+1,updated_at=now() WHERE recording_id=$1 AND channel_id=$2`, d.RecordingID, d.ChannelID)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
