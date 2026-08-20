BEGIN;

CREATE TABLE auth_rate_limits (
  key_hash bytea PRIMARY KEY,
  window_started_at timestamptz NOT NULL DEFAULT now(),
  attempt_count integer NOT NULL DEFAULT 1 CHECK (attempt_count > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (octet_length(key_hash) = 32)
);

CREATE INDEX auth_rate_limits_updated_at_idx
ON auth_rate_limits (updated_at);

COMMIT;
