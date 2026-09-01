-- Public, organizer-managed dates that do not already belong to a ticket
-- tier or another operational record (for example: tickets on sale or CFP
-- opening). Ticket price changes and the CFP close are derived in code from
-- their existing sources of truth.

CREATE TABLE conference_milestones (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
  label text NOT NULL,
  category text NOT NULL DEFAULT 'event',
  occurs_at timestamptz NOT NULL,
  url text NOT NULL DEFAULT '',
  published boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (label <> ''),
  CHECK (category IN ('tickets', 'talks', 'event', 'other'))
);

CREATE INDEX conference_milestones_public_timeline_idx
ON conference_milestones (conference_id, published, occurs_at, created_at);

CREATE TRIGGER conference_milestones_set_updated_at
BEFORE UPDATE ON conference_milestones
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
