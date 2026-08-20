BEGIN;

CREATE TABLE person_api_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  name text NOT NULL,
  token_selector text NOT NULL UNIQUE,
  token_hash bytea NOT NULL,
  scopes text[] NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (btrim(name) <> ''),
  CHECK (btrim(token_selector) <> ''),
  CHECK (octet_length(token_hash) = 32),
  CHECK (cardinality(scopes) > 0),
  CHECK (expires_at > created_at)
);

CREATE INDEX person_api_tokens_person_idx
ON person_api_tokens (person_id, created_at DESC, id);

CREATE INDEX person_api_tokens_active_idx
ON person_api_tokens (token_selector)
WHERE revoked_at IS NULL;

CREATE TRIGGER person_api_tokens_set_updated_at
BEFORE UPDATE ON person_api_tokens
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
