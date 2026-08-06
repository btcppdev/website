BEGIN;

ALTER TABLE person_merge_requests
DROP CONSTRAINT person_merge_requests_check1;

-- Requests created before recipient confirmation was introduced had already
-- been placed in the admin queue. Preserve them as confirmed legacy requests.
UPDATE person_merge_requests
SET confirmed_at = created_at
WHERE status = 'pending' AND confirmed_at IS NULL;

ALTER TABLE person_merge_requests
ADD CHECK (
  (status = 'awaiting_confirmation' AND reviewed_at IS NULL AND confirmed_at IS NULL) OR
  (status = 'pending' AND reviewed_at IS NULL AND confirmed_at IS NOT NULL) OR
  (status NOT IN ('awaiting_confirmation', 'pending') AND reviewed_at IS NOT NULL)
);

COMMIT;
