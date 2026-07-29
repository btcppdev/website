BEGIN;

-- Coordinators manage the hackathon rather than score projects. Preserve all
-- existing assignments as conference-scoped roles before removing the legacy
-- judge type.
INSERT INTO people_roles (person_id, scope, position)
SELECT DISTINCT judges.person_id, conferences.tag, 'hackathon'
FROM competition_judges judges
JOIN competitions ON competitions.id = judges.competition_id
JOIN conferences ON conferences.id = competitions.conference_id
WHERE judges.judge_type = 'coordinator'
ON CONFLICT DO NOTHING;

DELETE FROM competition_judges
WHERE judge_type = 'coordinator';

ALTER TABLE competition_judges
  DROP CONSTRAINT competition_judges_judge_type_check;

ALTER TABLE competition_judges
  ADD CONSTRAINT competition_judges_judge_type_check
  CHECK (judge_type IN ('expo', 'finals'));

COMMIT;
