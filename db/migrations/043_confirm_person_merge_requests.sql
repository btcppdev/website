BEGIN;

ALTER TABLE person_merge_requests
DROP CONSTRAINT person_merge_requests_status_check;

ALTER TABLE person_merge_requests
ADD COLUMN confirmation_token_hash bytea UNIQUE,
ADD COLUMN confirmation_expires_at timestamptz,
ADD COLUMN confirmed_at timestamptz;

ALTER TABLE person_merge_requests
ALTER COLUMN status SET DEFAULT 'awaiting_confirmation';

ALTER TABLE person_merge_requests
ADD CHECK (status IN ('awaiting_confirmation', 'pending', 'rejected', 'merged', 'reverted', 'superseded'));

DROP INDEX person_merge_requests_pending_pair_idx;

CREATE UNIQUE INDEX person_merge_requests_active_pair_idx
ON person_merge_requests (
  least(requester_person_id, target_person_id),
  greatest(requester_person_id, target_person_id)
)
WHERE status IN ('awaiting_confirmation', 'pending');

COMMIT;
