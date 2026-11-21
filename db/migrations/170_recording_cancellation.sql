-- A null target cancels either destination. Keep the original X timestamp and
-- IDs as a tombstone so streamctl can disable an already-installed timer.
ALTER TABLE recording_schedule_changes DROP CONSTRAINT recording_schedule_changes_check;
ALTER TABLE recording_schedule_changes ADD COLUMN broadcast_id text NOT NULL DEFAULT '';
ALTER TABLE recording_x_broadcasts DROP CONSTRAINT recording_x_broadcasts_status_check;
ALTER TABLE recording_x_broadcasts ADD CONSTRAINT recording_x_broadcasts_status_check
 CHECK(status IN ('creating','created','uploading_poster','poster_uploaded','finalizing','scheduled','failed','canceled'));
