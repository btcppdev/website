BEGIN;

CREATE TABLE person_nostr_credentials (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  pubkey_hex text,
  legacy_value text,
  verified_at timestamptz,
  linked_at timestamptz NOT NULL DEFAULT now(),
  last_login_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (pubkey_hex),
  CHECK (
    (pubkey_hex IS NOT NULL AND legacy_value IS NULL) OR
    (pubkey_hex IS NULL AND legacy_value IS NOT NULL)
  ),
  CHECK (pubkey_hex IS NULL OR pubkey_hex ~ '^[0-9a-f]{64}$'),
  CHECK (legacy_value IS NULL OR btrim(legacy_value) <> '')
);

CREATE INDEX person_nostr_credentials_person_idx
ON person_nostr_credentials (person_id, linked_at, id);

CREATE TRIGGER person_nostr_credentials_set_updated_at
BEFORE UPDATE ON person_nostr_credentials
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Preserve the Nostr keys that were already accepted for authentication.
-- They are promoted to canonical, signature-verified hex keys on first use.
INSERT INTO person_nostr_credentials (person_id, legacy_value)
SELECT id, btrim(nostr)
FROM people
WHERE btrim(nostr) <> '';

COMMIT;
