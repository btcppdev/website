BEGIN;

CREATE TABLE person_auth_security (
  person_id uuid PRIMARY KEY REFERENCES people(id) ON DELETE CASCADE,
  session_version bigint NOT NULL DEFAULT 1 CHECK (session_version > 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO person_auth_security (person_id)
SELECT id FROM people;

CREATE TRIGGER person_auth_security_set_updated_at
BEFORE UPDATE ON person_auth_security
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE person_password_credentials (
  person_id uuid PRIMARY KEY REFERENCES people(id) ON DELETE CASCADE,
  password_hash text NOT NULL,
  failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
  locked_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  password_changed_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (password_hash <> '')
);

CREATE TRIGGER person_password_credentials_set_updated_at
BEFORE UPDATE ON person_password_credentials
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE password_reset_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  requested_email citext NOT NULL,
  token_hash bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > created_at)
);

CREATE INDEX password_reset_tokens_person_idx
ON password_reset_tokens (person_id, created_at DESC);

CREATE INDEX password_reset_tokens_expiry_idx
ON password_reset_tokens (expires_at)
WHERE consumed_at IS NULL;

COMMIT;
