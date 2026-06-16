BEGIN;

CREATE TABLE IF NOT EXISTS satellite_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  event_url text NOT NULL DEFAULT '',
  event_type text NOT NULL DEFAULT '',
  starts_at timestamptz,
  ends_at timestamptz,
  location text NOT NULL DEFAULT '',
  image_url text NOT NULL DEFAULT '',
  host_name text NOT NULL DEFAULT '',
  host_url text NOT NULL DEFAULT '',
  host_logo_url text NOT NULL DEFAULT '',
  submitter_email citext,
  status text NOT NULL DEFAULT 'draft',
  notes text NOT NULL DEFAULT '',
  published_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (title <> ''),
  CHECK (status IN ('draft', 'submitted', 'published')),
  CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at >= starts_at)
);

CREATE INDEX IF NOT EXISTS satellite_events_conference_status_idx
ON satellite_events (conference_id, status, starts_at);

DROP TRIGGER IF EXISTS satellite_events_set_updated_at ON satellite_events;
CREATE TRIGGER satellite_events_set_updated_at
BEFORE UPDATE ON satellite_events
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
