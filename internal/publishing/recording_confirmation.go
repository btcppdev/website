package publishing

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func (w RecordingScheduler) reconcileYouTube(ctx context.Context, conn *pgxpool.Conn) error {
	if w.Confirm == nil {
		return nil
	}
	// Recheck uncertain YouTube outcomes at most every five minutes. Never
	// recreate broadcasts, change visibility, or reapply a requested schedule.
	rows, err := conn.Query(ctx, `SELECT recording_id::text,channel_id,target_at,youtube_url,file_uri,version
 FROM recording_schedule_changes WHERE channel_id='youtube' AND state='needs_review'
 AND updated_at < now()-interval '5 minutes' AND (target_at IS NULL OR target_at>now())
 ORDER BY updated_at LIMIT 10`)
	if err != nil {
		return err
	}
	var changes []RecordingChange
	for rows.Next() {
		var d RecordingChange
		if err = rows.Scan(&d.RecordingID, &d.ChannelID, &d.TargetAt, &d.YouTubeURL, &d.FileURI, &d.Version); err != nil {
			rows.Close()
			return err
		}
		changes = append(changes, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range changes {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		confirmed, checkErr := w.Confirm(checkCtx, d)
		cancel()
		if confirmed && checkErr == nil {
			err = completeRecordingChange(ctx, conn, d, "needs_review")
			if err != nil && !errors.Is(err, ErrChanged) {
				return err
			}
		}
		// Keep the original diagnostic and reservation if the result still does
		// not match. Rate-limit reads even on provider errors or changed media.
		if _, err = conn.Exec(ctx, `UPDATE recording_schedule_changes SET updated_at=now()
 WHERE recording_id=$1 AND channel_id='youtube' AND state='needs_review' AND version=$2`, d.RecordingID, d.Version); err != nil {
			return err
		}
	}
	return nil
}

// Confirmed source fields and operation completion advance atomically. The
// version and media guards prevent a delayed read from confirming a newer edit.
func completeRecordingChange(ctx context.Context, conn *pgxpool.Conn, d RecordingChange, state string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockRecordings(ctx, tx); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE recording_schedule_changes SET state='applied',error='',lease_until=NULL,version=version+1,updated_at=now()
 WHERE recording_id=$1 AND channel_id=$2 AND version=$3 AND state=$4`, d.RecordingID, d.ChannelID, d.Version, state)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrChanged
	}
	if d.ChannelID == "youtube" {
		tag, err = tx.Exec(ctx, `UPDATE recordings SET publish_at=$2 WHERE id=$1 AND youtube_url=$3 AND file_uri=$4`, d.RecordingID, d.TargetAt, d.YouTubeURL, d.FileURI)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrChanged
		}
		if _, err = tx.Exec(ctx, `UPDATE social_posts SET scheduled_at=$2,status=CASE WHEN $2::timestamptz IS NULL THEN 'canceled' ELSE 'scheduled' END,error='' WHERE ref='recording:'||$1||':youtube'`, d.RecordingID, d.TargetAt); err != nil {
			return err
		}
		privacy := "private"
		if d.TargetAt == nil {
			privacy = "unlisted"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO recording_youtube_status(recording_id,youtube_url,privacy_status)
 VALUES($1,$2,$3) ON CONFLICT(recording_id) DO UPDATE SET youtube_url=EXCLUDED.youtube_url,privacy_status=EXCLUDED.privacy_status,checked_at=now()`, d.RecordingID, d.YouTubeURL, privacy); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
