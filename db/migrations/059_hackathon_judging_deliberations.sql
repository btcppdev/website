CREATE TABLE judge_event_deliberations (
  judge_event_id uuid PRIMARY KEY REFERENCES judge_events(id) ON DELETE CASCADE,
  project_order uuid[] NOT NULL DEFAULT ARRAY[]::uuid[],
  advance_count integer,
  revision bigint NOT NULL DEFAULT 1,
  updated_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (advance_count IS NULL OR advance_count > 0),
  CHECK (revision > 0)
);
