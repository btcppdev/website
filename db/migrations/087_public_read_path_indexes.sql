BEGIN;

CREATE INDEX conf_talks_active_conference_schedule_idx
ON conf_talks (conference_id, scheduled_start, id)
WHERE archived_at IS NULL;

CREATE INDEX sponsorships_conferences_conference_idx
ON sponsorships_conferences (conference_id, sponsorship_id);

CREATE INDEX recordings_youtube_publish_queue_idx
ON recordings (publish_at, id)
WHERE publish_at IS NOT NULL
  AND btrim(file_uri) <> ''
  AND btrim(youtube_url) = '';

COMMIT;
