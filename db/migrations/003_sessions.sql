CREATE TABLE IF NOT EXISTS sessions (
  token text PRIMARY KEY,
  data bytea NOT NULL,
  expiry timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions (expiry);
