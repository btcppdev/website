CREATE TABLE person_badge_presentations (
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  badge_ref text NOT NULL,
  hidden boolean NOT NULL DEFAULT false,
  featured_position smallint,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (person_id, badge_ref),
  CHECK (badge_ref <> '' AND length(badge_ref) <= 512),
  CHECK (featured_position IS NULL OR featured_position BETWEEN 1 AND 6),
  CHECK (NOT hidden OR featured_position IS NULL)
);

CREATE UNIQUE INDEX person_badge_presentations_featured_position_idx
ON person_badge_presentations (person_id, featured_position)
WHERE featured_position IS NOT NULL;
