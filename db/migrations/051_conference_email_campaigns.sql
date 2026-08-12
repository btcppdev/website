BEGIN;

ALTER TABLE conferences
  ADD COLUMN speaker_dinner_start timestamptz,
  ADD COLUMN speaker_dinner_location text NOT NULL DEFAULT 'Location TBD',
  ADD COLUMN speaker_dinner_notes text NOT NULL DEFAULT '';

ALTER TABLE missives
  ADD COLUMN conference_id uuid REFERENCES conferences(id) ON DELETE CASCADE;

CREATE INDEX missives_conference_idx
ON missives (conference_id, public_uid DESC)
WHERE conference_id IS NOT NULL;

CREATE TABLE conference_email_campaigns (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
  kind text NOT NULL,
  audience text NOT NULL,
  title text NOT NULL,
  markdown text NOT NULL DEFAULT '',
  enabled boolean NOT NULL DEFAULT true,
  send_time time NOT NULL DEFAULT '10:00',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (conference_id, kind),
  CHECK (kind <> ''),
  CHECK (audience <> ''),
  CHECK (title <> '')
);

CREATE TRIGGER conference_email_campaigns_set_updated_at
BEFORE UPDATE ON conference_email_campaigns
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE conference_email_occurrences (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id uuid NOT NULL REFERENCES conference_email_campaigns(id) ON DELETE CASCADE,
  occurrence_key text NOT NULL,
  build_at timestamptz NOT NULL,
  send_at timestamptz NOT NULL,
  missive_id uuid REFERENCES missives(id) ON DELETE SET NULL,
  target_key text NOT NULL DEFAULT '',
  target_email citext,
  status text NOT NULL DEFAULT 'planned',
  claimed_at timestamptz,
  built_at timestamptz,
  queued_at timestamptz,
  sent_at timestamptz,
  skipped_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (campaign_id, occurrence_key),
  CHECK (send_at >= build_at),
  CHECK (status IN ('planned', 'building', 'draft', 'sending', 'sent', 'skipped', 'paused', 'cancelled', 'failed'))
);

CREATE INDEX conference_email_occurrences_due_idx
ON conference_email_occurrences (status, build_at, send_at);

CREATE TRIGGER conference_email_occurrences_set_updated_at
BEFORE UPDATE ON conference_email_occurrences
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE conference_email_deliveries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  occurrence_id uuid NOT NULL REFERENCES conference_email_occurrences(id) ON DELETE CASCADE,
  recipient_key text NOT NULL,
  email citext NOT NULL,
  job_key text NOT NULL,
  status text NOT NULL DEFAULT 'planned',
  queued_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (occurrence_id, recipient_key),
  UNIQUE (job_key),
  CHECK (recipient_key <> ''),
  CHECK (email <> ''),
  CHECK (job_key <> ''),
  CHECK (status IN ('planned', 'queued', 'failed', 'cancelled'))
);

CREATE TRIGGER conference_email_deliveries_set_updated_at
BEFORE UPDATE ON conference_email_deliveries
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
