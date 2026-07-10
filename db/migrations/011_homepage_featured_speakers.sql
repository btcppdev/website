BEGIN;

CREATE TABLE homepage_featured_speakers (
  position integer PRIMARY KEY,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (position BETWEEN 1 AND 8)
);

CREATE TRIGGER homepage_featured_speakers_set_updated_at
BEFORE UPDATE ON homepage_featured_speakers
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
