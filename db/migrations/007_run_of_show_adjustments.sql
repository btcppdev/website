BEGIN;

CREATE TABLE IF NOT EXISTS run_of_show_adjustments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
  venue text NOT NULL DEFAULT '',
  anchor_kind text NOT NULL,
  anchor_id text NOT NULL,
  delay_minutes integer NOT NULL,
  propagation_mode text NOT NULL DEFAULT 'until_next_anchor',
  note text NOT NULL DEFAULT '',
  archived_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (anchor_kind IN ('talk', 'info')),
  CHECK (propagation_mode IN ('until_next_anchor', 'push_following', 'item_only')),
  CHECK (delay_minutes BETWEEN -240 AND 240)
);

CREATE UNIQUE INDEX IF NOT EXISTS run_of_show_adjustments_one_active_anchor_idx
ON run_of_show_adjustments (conference_id, anchor_kind, anchor_id)
WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS run_of_show_adjustments_conf_active_idx
ON run_of_show_adjustments (conference_id, archived_at);

CREATE TRIGGER run_of_show_adjustments_set_updated_at
BEFORE UPDATE ON run_of_show_adjustments
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
