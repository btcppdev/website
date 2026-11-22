-- A slot swap may need one temporary future provider time to break a cycle.
-- target_at remains the reviewed final time; staging_at is reserved until the
-- provider confirms the temporary move. Existing schedules are unchanged.
ALTER TABLE recording_schedule_changes ADD COLUMN staging_at timestamptz;
ALTER TABLE recording_schedule_changes ADD CONSTRAINT recording_schedule_staging_target
 CHECK(staging_at IS NULL OR target_at IS NOT NULL);
