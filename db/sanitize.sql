BEGIN;

UPDATE conferences
SET orient_cal_notif = '';

UPDATE person_emails
SET email = ('__sanitizing_person_email_' || id || '@example.invalid')::citext;

WITH numbered AS (
  SELECT id, row_number() OVER (ORDER BY id) AS rn
  FROM person_emails
)
UPDATE person_emails AS email
SET email = ('person+' || numbered.rn || '@example.test')::citext
FROM numbered
WHERE email.id = numbered.id;

DELETE FROM person_email_verifications;

UPDATE person_email_conflicts
SET email = ('conflict+' || md5(email::text) || '@example.test')::citext;

UPDATE people
SET phone = '',
  signal = '',
  telegram = '',
  twitter_handle = '',
  nostr = '',
  github_url = '',
  instagram = '',
  linkedin = '',
  website_url = '',
  tshirt = '';

UPDATE organizations
SET email = NULL,
  notes = '';

UPDATE sponsorships
SET notes = '';

UPDATE proposals
SET comments = '',
  invite_token = '';

UPDATE speaker_confs
SET coming_from = '',
  availability = '{}',
  visa = '',
  org_photo_path = '';

UPDATE conf_talks
SET production_notes = '',
  cal_notif = '';

UPDATE recordings
SET file_uri = '';

UPDATE social_posts
SET error = '',
  error_fingerprint = '';

UPDATE discounts
SET code_name = ('__SANITIZING_DISCOUNT__' || id)::citext;

WITH numbered AS (
  SELECT id, row_number() OVER (ORDER BY id) AS rn
  FROM discounts
)
UPDATE discounts AS d
SET code_name = ('SANITIZED-' || numbered.rn)::citext,
  affiliate_email = NULL
FROM numbered
WHERE d.id = numbered.id;

UPDATE registrations
SET ref_id = '__sanitizing_registration__' || id;

WITH numbered AS (
  SELECT id, row_number() OVER (ORDER BY id) AS rn
  FROM registrations
)
UPDATE registrations AS r
SET ref_id = 'sanitized-registration-' || numbered.rn,
  checkout_id = '',
  email = ('registration+' || numbered.rn || '@example.test')::citext
FROM numbered
WHERE r.id = numbered.id;

WITH numbered AS (
  SELECT id, row_number() OVER (ORDER BY id) AS rn
  FROM affiliate_usages
)
UPDATE affiliate_usages AS au
SET code_name_snapshot = ('SANITIZED-' || numbered.rn)::citext,
  affiliate_email = ('affiliate+' || numbered.rn || '@example.test')::citext
FROM numbered
WHERE au.id = numbered.id;

WITH numbered AS (
  SELECT id, row_number() OVER (ORDER BY id) AS rn
  FROM volunteers
)
UPDATE volunteers AS v
SET availability = '{}',
  contact_at = '',
  comments = '',
  discovered_via = '',
  hometown = '',
  captcha = 0
FROM numbered
WHERE v.id = numbered.id;

-- Production snapshots from before volunteer/person normalization still have
-- profile and contact columns on volunteers. Scrub those before later local
-- migrations copy them into people. Keep equal source emails equal so the
-- identity backfill continues to exercise duplicate-application behavior.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'volunteers' AND column_name = 'email'
  ) THEN
    EXECUTE $sanitize_volunteers$
      WITH numbered AS (
        SELECT id, row_number() OVER (ORDER BY id) AS rn
        FROM volunteers
      )
      UPDATE volunteers AS volunteer
      SET name = 'Volunteer ' || numbered.rn,
        email = ('volunteer+' || md5(lower(volunteer.email::text)) || '@example.test')::citext,
        phone = '',
        signal = '',
        twitter_handle = '',
        nostr = '',
        shirt = ''
      FROM numbered
      WHERE volunteer.id = numbered.id
    $sanitize_volunteers$;
  END IF;
END
$$;

UPDATE work_shifts
SET cal_notif = '';

UPDATE volunteer_info
SET orient_link_url = '',
  notes = '';

UPDATE subscribers
SET email = ('__sanitizing_subscriber__' || id || '@example.test')::citext;

WITH numbered AS (
  SELECT id, row_number() OVER (ORDER BY id) AS rn
  FROM subscribers
)
UPDATE subscribers AS s
SET email = ('subscriber+' || numbered.rn || '@example.test')::citext
FROM numbered
WHERE s.id = numbered.id;

COMMIT;
