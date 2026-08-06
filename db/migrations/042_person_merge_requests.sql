BEGIN;

CREATE TABLE person_merge_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- Person IDs intentionally remain as audit identifiers after one side is
  -- deleted by a completed merge.
  requester_person_id uuid NOT NULL,
  requester_name text NOT NULL,
  requester_email citext NOT NULL,
  target_person_id uuid NOT NULL,
  target_name text NOT NULL,
  target_email citext NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  reviewed_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  merge_event_id uuid REFERENCES person_merge_events(id) ON DELETE SET NULL,
  review_note text NOT NULL DEFAULT '',
  reviewed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (requester_person_id <> target_person_id),
  CHECK (target_email <> ''),
  CHECK (status IN ('pending', 'rejected', 'merged', 'reverted', 'superseded')),
  CHECK ((status = 'pending' AND reviewed_at IS NULL) OR
         (status <> 'pending' AND reviewed_at IS NOT NULL))
);

CREATE UNIQUE INDEX person_merge_requests_pending_pair_idx
ON person_merge_requests (
  least(requester_person_id, target_person_id),
  greatest(requester_person_id, target_person_id)
)
WHERE status = 'pending';

CREATE INDEX person_merge_requests_requester_idx
ON person_merge_requests (requester_person_id, created_at DESC);

CREATE INDEX person_merge_requests_status_idx
ON person_merge_requests (status, created_at);

CREATE TRIGGER person_merge_requests_set_updated_at
BEFORE UPDATE ON person_merge_requests
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
