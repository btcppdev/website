BEGIN;

CREATE TABLE person_passkey_credentials (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  credential_id bytea NOT NULL UNIQUE,
  credential bytea NOT NULL,
  display_name text NOT NULL DEFAULT 'Passkey',
  created_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (octet_length(credential_id) > 0),
  CHECK (octet_length(credential) > 28),
  CHECK (btrim(display_name) <> '')
);

CREATE INDEX person_passkey_credentials_person_idx
ON person_passkey_credentials (person_id, created_at, id);

CREATE TRIGGER person_passkey_credentials_set_updated_at
BEFORE UPDATE ON person_passkey_credentials
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
