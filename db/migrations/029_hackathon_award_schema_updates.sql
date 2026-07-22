BEGIN;

ALTER TABLE awards
  ADD COLUMN award_type text NOT NULL DEFAULT 'ranked',
  ADD COLUMN judging_instructions text NOT NULL DEFAULT '',
  ADD COLUMN award_rank integer;

UPDATE awards
SET max_awardees = 1
WHERE max_awardees IS NULL;

ALTER TABLE awards
  ALTER COLUMN max_awardees SET DEFAULT 1,
  ALTER COLUMN max_awardees SET NOT NULL;

ALTER TABLE awards
  DROP CONSTRAINT IF EXISTS awards_max_awardees_check,
  ADD CONSTRAINT awards_award_type_check CHECK (award_type IN ('ranked', 'sponsor')),
  ADD CONSTRAINT awards_award_rank_check CHECK (award_rank IS NULL OR award_rank > 0),
  ADD CONSTRAINT awards_max_awardees_check CHECK (max_awardees > 0);

CREATE TABLE award_judges (
  award_id uuid NOT NULL REFERENCES awards(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (award_id, person_id)
);

CREATE INDEX award_judges_person_idx ON award_judges (person_id);

CREATE TABLE award_votes (
  award_id uuid NOT NULL REFERENCES awards(id) ON DELETE CASCADE,
  judge_person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  notes text NOT NULL DEFAULT '',
  submitted_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (award_id, judge_person_id)
);

CREATE INDEX award_votes_project_idx ON award_votes (project_id);

CREATE TRIGGER award_votes_set_updated_at
BEFORE UPDATE ON award_votes
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
