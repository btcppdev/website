ALTER TABLE satellite_events
ADD COLUMN submitter_person_id uuid REFERENCES people(id) ON DELETE SET NULL;

CREATE INDEX satellite_events_submitter_person_idx
ON satellite_events (submitter_person_id, created_at DESC);

UPDATE satellite_events event
SET submitter_person_id = email.person_id
FROM person_emails email
WHERE event.submitter_person_id IS NULL
  AND event.submitter_email = email.email;
