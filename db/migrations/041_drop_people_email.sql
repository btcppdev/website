BEGIN;

-- Reconcile any people created by old application versions after the original
-- alias backfill. Ambiguous addresses fail closed instead of being assigned to
-- an arbitrary person.
WITH candidates AS (
  SELECT id AS person_id, email
  FROM people
  WHERE email IS NOT NULL AND email <> ''
  UNION
  SELECT person_id, email
  FROM person_emails
),
ambiguous AS (
  SELECT email
  FROM candidates
  GROUP BY email
  HAVING count(DISTINCT person_id) > 1
)
INSERT INTO person_email_conflicts (email, person_id)
SELECT candidates.email, candidates.person_id
FROM candidates
JOIN ambiguous USING (email)
ON CONFLICT DO NOTHING;

WITH candidates AS (
  SELECT id AS person_id, email
  FROM people
  WHERE email IS NOT NULL AND email <> ''
  UNION
  SELECT person_id, email
  FROM person_emails
),
ambiguous AS (
  SELECT email
  FROM candidates
  GROUP BY email
  HAVING count(DISTINCT person_id) > 1
)
DELETE FROM person_emails
WHERE email IN (SELECT email FROM ambiguous);

WITH candidates AS (
  SELECT id AS person_id, email
  FROM people
  WHERE email IS NOT NULL AND email <> ''
),
unambiguous AS (
  SELECT email
  FROM candidates
  GROUP BY email
  HAVING count(DISTINCT person_id) = 1
)
INSERT INTO person_emails (person_id, email, is_primary, verified_at)
SELECT candidates.person_id, candidates.email,
  NOT EXISTS (
    SELECT 1 FROM person_emails existing
    WHERE existing.person_id = candidates.person_id AND existing.is_primary
  ),
  now()
FROM candidates
JOIN unambiguous USING (email)
ON CONFLICT (email) DO NOTHING;

ALTER TABLE people DROP COLUMN email;

COMMIT;
