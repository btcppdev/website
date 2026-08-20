BEGIN;

CREATE TABLE person_oauth_identities (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  provider text NOT NULL,
  provider_subject text NOT NULL,
  provider_username text NOT NULL DEFAULT '',
  provider_email citext,
  provider_email_verified boolean NOT NULL DEFAULT false,
  avatar_url text NOT NULL DEFAULT '',
  linked_at timestamptz NOT NULL DEFAULT now(),
  last_login_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, provider_subject),
  CHECK (provider = lower(provider) AND provider <> ''),
  CHECK (provider_subject <> '')
);

CREATE INDEX person_oauth_identities_person_idx
ON person_oauth_identities (person_id, provider, linked_at);

CREATE TRIGGER person_oauth_identities_set_updated_at
BEFORE UPDATE ON person_oauth_identities
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE auth_audit_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  method text NOT NULL DEFAULT '',
  event text NOT NULL,
  remote_address text NOT NULL DEFAULT '',
  user_agent text NOT NULL DEFAULT '',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (event <> '')
);

CREATE INDEX auth_audit_events_person_idx
ON auth_audit_events (person_id, created_at DESC);

CREATE INDEX auth_audit_events_event_idx
ON auth_audit_events (event, created_at DESC);

COMMIT;
