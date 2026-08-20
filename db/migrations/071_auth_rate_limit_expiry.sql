BEGIN;

ALTER TABLE auth_rate_limits
ADD COLUMN expires_at timestamptz;

-- Migration 069 did not record the window duration. Keep existing buckets for
-- at most one more day; subsequent attempts set their exact window expiry.
UPDATE auth_rate_limits
SET expires_at = updated_at + interval '1 day';

ALTER TABLE auth_rate_limits
ALTER COLUMN expires_at SET NOT NULL;

DROP INDEX auth_rate_limits_updated_at_idx;

CREATE INDEX auth_rate_limits_expires_at_idx
ON auth_rate_limits (expires_at);

COMMIT;
