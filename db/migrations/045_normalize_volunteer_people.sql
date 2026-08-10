BEGIN;

-- Migration 039 linked volunteer applications where an email already had one
-- unambiguous owner. Preserve every remaining historical applicant without
-- guessing between conflicted identities: create one person per unmatched
-- volunteer email and leave conflicted addresses in the admin merge queue.
CREATE TEMP TABLE volunteer_identity_backfill (
  email citext PRIMARY KEY,
  person_id uuid NOT NULL
) ON COMMIT DROP;

INSERT INTO volunteer_identity_backfill (email, person_id)
SELECT unmatched.email, gen_random_uuid()
FROM (
  SELECT DISTINCT lower(btrim(email::text))::citext AS email
  FROM volunteers
  WHERE person_id IS NULL
) unmatched;

WITH latest AS (
  SELECT DISTINCT ON (lower(btrim(volunteer.email::text)))
    lower(btrim(volunteer.email::text))::citext AS email,
    volunteer.name,
    volunteer.phone,
    volunteer.signal,
    volunteer.twitter_handle,
    volunteer.nostr,
    volunteer.shirt
  FROM volunteers volunteer
  WHERE volunteer.person_id IS NULL
  ORDER BY lower(btrim(volunteer.email::text)), volunteer.created_at DESC, volunteer.id
)
INSERT INTO people (
  id, name, phone, signal, twitter_handle, nostr, tshirt
)
SELECT backfill.person_id, latest.name, latest.phone, latest.signal,
  latest.twitter_handle, latest.nostr, latest.shirt
FROM volunteer_identity_backfill backfill
JOIN latest USING (email);

INSERT INTO person_emails (person_id, email, is_primary, verified_at)
SELECT backfill.person_id, backfill.email, true, now()
FROM volunteer_identity_backfill backfill
WHERE NOT EXISTS (
    SELECT 1 FROM person_emails existing WHERE existing.email = backfill.email
  )
  AND NOT EXISTS (
    SELECT 1 FROM person_email_conflicts conflict WHERE conflict.email = backfill.email
  );

INSERT INTO person_email_conflicts (email, person_id)
SELECT backfill.email, backfill.person_id
FROM volunteer_identity_backfill backfill
WHERE EXISTS (
    SELECT 1 FROM person_email_conflicts conflict WHERE conflict.email = backfill.email
  )
ON CONFLICT DO NOTHING;

UPDATE volunteers volunteer
SET person_id = backfill.person_id
FROM volunteer_identity_backfill backfill
WHERE volunteer.person_id IS NULL
  AND lower(btrim(volunteer.email::text))::citext = backfill.email;

-- Volunteer applications sometimes contain the only copy of older contact
-- details. Copy the newest non-empty value into an empty canonical field, but
-- never overwrite profile data the person has already maintained.
WITH latest AS (
  SELECT DISTINCT ON (person_id) person_id, phone
  FROM volunteers
  WHERE btrim(phone) <> ''
  ORDER BY person_id, created_at DESC, id
)
UPDATE people person
SET phone = latest.phone
FROM latest
WHERE person.id = latest.person_id AND btrim(person.phone) = '';

WITH latest AS (
  SELECT DISTINCT ON (person_id) person_id, signal
  FROM volunteers
  WHERE btrim(signal) <> ''
  ORDER BY person_id, created_at DESC, id
)
UPDATE people person
SET signal = latest.signal
FROM latest
WHERE person.id = latest.person_id AND btrim(person.signal) = '';

WITH latest AS (
  SELECT DISTINCT ON (person_id) person_id, twitter_handle
  FROM volunteers
  WHERE btrim(twitter_handle) <> ''
  ORDER BY person_id, created_at DESC, id
)
UPDATE people person
SET twitter_handle = latest.twitter_handle
FROM latest
WHERE person.id = latest.person_id AND btrim(person.twitter_handle) = '';

WITH latest AS (
  SELECT DISTINCT ON (person_id) person_id, nostr
  FROM volunteers
  WHERE btrim(nostr) <> ''
  ORDER BY person_id, created_at DESC, id
)
UPDATE people person
SET nostr = latest.nostr
FROM latest
WHERE person.id = latest.person_id AND btrim(person.nostr) = '';

WITH latest AS (
  SELECT DISTINCT ON (person_id) person_id, shirt
  FROM volunteers
  WHERE btrim(shirt) <> ''
  ORDER BY person_id, created_at DESC, id
)
UPDATE people person
SET tshirt = latest.shirt
FROM latest
WHERE person.id = latest.person_id AND btrim(person.tshirt) = '';

-- Complete registration ownership while the historical volunteer email is
-- still present. Only unambiguous volunteer identities are eligible.
WITH volunteer_owner AS (
  SELECT lower(btrim(volunteer.email::text))::citext AS email,
    min(volunteer.person_id::text)::uuid AS person_id
  FROM volunteers volunteer
  WHERE NOT EXISTS (
    SELECT 1
    FROM person_email_conflicts conflict
    WHERE conflict.email = lower(btrim(volunteer.email::text))::citext
  )
    AND EXISTS (
      SELECT 1
      FROM person_emails alias
      WHERE alias.person_id = volunteer.person_id
        AND alias.email = lower(btrim(volunteer.email::text))::citext
    )
  GROUP BY lower(btrim(volunteer.email::text))::citext
  HAVING count(DISTINCT person_id) = 1
)
UPDATE registrations registration
SET person_id = owner.person_id
FROM volunteer_owner owner
WHERE registration.person_id IS NULL
  AND lower(btrim(registration.email::text))::citext = owner.email;

ALTER TABLE volunteers
  ALTER COLUMN person_id SET NOT NULL;

DROP INDEX IF EXISTS volunteers_email_idx;

ALTER TABLE volunteers
  DROP COLUMN name,
  DROP COLUMN email,
  DROP COLUMN phone,
  DROP COLUMN signal,
  DROP COLUMN twitter_handle,
  DROP COLUMN nostr,
  DROP COLUMN shirt;

COMMIT;
