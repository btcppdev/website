package publishing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"btcpp-web/external/getters"
)

type RecordingChange struct {
	Staging     bool
	FinalAt     *time.Time
	BroadcastID string
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
	// Confirm only reads provider state; uncertain operations must not be repeated.
	Confirm func(context.Context, RecordingChange) (bool, error)
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
	if e = w.reconcileRecordings(ctx, conn); e != nil {
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
		// A batch may reuse another item's old slot. Only send a move once
		// that slot is actually free, not merely promised by a pending job.
		rows, e := tx.Query(ctx, `SELECT recording_id::text,channel_id,coalesce(staging_at,target_at),youtube_url,file_uri,version,broadcast_id,staging_at IS NOT NULL,target_at FROM recording_schedule_changes WHERE state='pending' ORDER BY updated_at,recording_id,channel_id FOR UPDATE SKIP LOCKED`)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		var pending []RecordingChange
		for rows.Next() {
			var d RecordingChange
			if e = rows.Scan(&d.RecordingID, &d.ChannelID, &d.TargetAt, &d.YouTubeURL, &d.FileURI, &d.Version, &d.BroadcastID, &d.Staging, &d.FinalAt); e != nil {
				break
			}
			pending = append(pending, d)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if len(pending) == 0 {
			tx.Rollback(ctx)
			return nil
		}
		items, e := recordingItems(ctx, tx)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		reservations, e := getters.RecordingScheduleReservations(ctx, tx)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		var d RecordingChange
		ready := false
		for _, candidate := range pending {
			item, itemErr := recordingByID(items, recordingDestinationID(candidate.RecordingID, candidate.ChannelID))
			reason := ""
			if itemErr != nil {
				reason = itemErr.Error()
			} else if candidate.TargetAt == nil {
				reason = validateRecordingCancellation(item, candidate, time.Now())
			} else if item.Reason != "" {
				reason = item.Reason
			} else if item.YouTubeURL != candidate.YouTubeURL || item.FileURI != candidate.FileURI {
				reason = "The recording media changed. Review and schedule the replacement again."
			} else if !candidate.TargetAt.After(time.Now().Add(time.Minute)) || (candidate.FinalAt != nil && !candidate.FinalAt.After(time.Now().Add(time.Minute))) {
				reason = "The requested slot is too close or has passed. Choose a future publication slot."
			}
			if reason == "" && candidate.TargetAt != nil {
				conflict, conflictErr := getters.RecordingScheduleConflictForRecording(candidate.RecordingID, *candidate.TargetAt, item.DurationSeconds, reservations)
				if conflictErr != nil {
					reason = conflictErr.Error()
				} else if conflict != nil {
					_, e = tx.Exec(ctx, `UPDATE recording_schedule_changes SET error=$3 WHERE recording_id=$1 AND channel_id=$2`, candidate.RecordingID, candidate.ChannelID, "Waiting for "+conflict.Label()+" to finish moving out of this slot")
					if e != nil {
						tx.Rollback(ctx)
						return e
					}
					continue
				}
			}
			if reason != "" {
				_, e = tx.Exec(ctx, `UPDATE recording_schedule_changes SET state='failed',error=$3,version=version+1,updated_at=now() WHERE recording_id=$1 AND channel_id=$2`, candidate.RecordingID, candidate.ChannelID, reason)
				if e != nil {
					tx.Rollback(ctx)
					return e
				}
				continue
			}
			d = candidate
			ready = true
			break
		}
		if !ready {
			return tx.Commit(ctx)
		}
		_, e = tx.Exec(ctx, `UPDATE recording_schedule_changes SET state='syncing',error='',lease_until=now()+interval '5 minutes',version=version+1,updated_at=now() WHERE recording_id=$1 AND channel_id=$2`, d.RecordingID, d.ChannelID)
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
		if e = completeRecordingChange(ctx, conn, d, "syncing"); e != nil {
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
	if d.Excluded && !d.CancelPending {
		return fmt.Errorf("Restore this item before retrying its schedule")
	}
	if d.State != "failed" && d.State != "needs_review" {
		return fmt.Errorf("This schedule change is not awaiting retry")
	}
	if d.RetryConfirmation && !confirmedNotCreated {
		return fmt.Errorf("Check X Studio first and confirm that no broadcast was created before retrying")
	}
	_, e = tx.Exec(ctx, `UPDATE recording_schedule_changes SET state='pending',error='',lease_until=NULL,version=version+1,updated_at=now() WHERE recording_id=$1 AND channel_id=$2`, d.RecordingID, d.ChannelID)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
