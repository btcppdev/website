BEGIN;

CREATE TABLE volunteer_application_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email citext NOT NULL,
  payload jsonb NOT NULL,
  token_hash bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (email <> ''),
  CHECK (expires_at > created_at)
);

CREATE INDEX volunteer_application_requests_pending_idx
ON volunteer_application_requests (email, expires_at DESC)
WHERE consumed_at IS NULL;

COMMIT;
