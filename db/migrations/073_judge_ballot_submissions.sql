CREATE TABLE judge_ballot_submissions (
  judge_event_id uuid NOT NULL REFERENCES judge_events(id) ON DELETE CASCADE,
  judge_person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  first_submitted_at timestamptz NOT NULL DEFAULT now(),
  last_submitted_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (judge_event_id, judge_person_id),
  CHECK (last_submitted_at >= first_submitted_at)
);

INSERT INTO judge_ballot_submissions (
  judge_event_id,
  judge_person_id,
  first_submitted_at,
  last_submitted_at
)
SELECT
  judge_event_id,
  judge_person_id,
  min(COALESCE(submitted_at, created_at)),
  max(COALESCE(submitted_at, updated_at, created_at))
FROM scorecards
GROUP BY judge_event_id, judge_person_id
ON CONFLICT (judge_event_id, judge_person_id) DO NOTHING;
