-- Durable schedule changes. Existing confirmed YouTube and X schedules stay in
-- their source tables; migration makes no provider calls and changes no times.
CREATE TABLE recording_schedule_changes (
 recording_id uuid NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
 channel_id text NOT NULL REFERENCES publishing_channels(id) CHECK(channel_id IN ('youtube','broadcasts')),
 target_at timestamptz,
 previous_at timestamptz,
 state text NOT NULL CHECK(state IN ('pending','syncing','applied','failed','needs_review')),
 youtube_url text NOT NULL DEFAULT '',
 file_uri text NOT NULL DEFAULT '',
 error text NOT NULL DEFAULT '',
 version integer NOT NULL DEFAULT 1,
 lease_until timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(recording_id,channel_id),
 CHECK(channel_id='youtube' OR target_at IS NOT NULL)
);
CREATE INDEX recording_schedule_changes_pending ON recording_schedule_changes(updated_at) WHERE state='pending';
-- Broadcast slots start with the existing YouTube cadence, then can be edited
-- independently in Publishing. Existing broadcast times are never shifted.
UPDATE publishing_channels SET timezone=coalesce((SELECT min(timezone) FROM youtube_publish_slots WHERE active),timezone),
 slots=coalesce((SELECT jsonb_agg(jsonb_build_object('day',weekday,'time',to_char(publish_time,'HH24:MI')) ORDER BY weekday,publish_time) FROM youtube_publish_slots WHERE active),'[]'::jsonb)
WHERE id='broadcasts' AND slots='[]'::jsonb;
