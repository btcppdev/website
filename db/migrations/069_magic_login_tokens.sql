BEGIN;

CREATE TABLE magic_login_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email citext NOT NULL,
  token_hash bytea NOT NULL UNIQUE,
  next_path text NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (octet_length(token_hash) = 32),
  CHECK (btrim(email::text) <> ''),
  CHECK (left(next_path, 1) = '/' AND left(next_path, 2) <> '//'),
  CHECK (expires_at > created_at)
);

CREATE INDEX magic_login_tokens_expiry_idx
ON magic_login_tokens (expires_at)
WHERE consumed_at IS NULL;

COMMIT;
