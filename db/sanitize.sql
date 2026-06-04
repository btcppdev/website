BEGIN;

UPDATE conferences
SET orient_cal_notif = '';

WITH numbered AS (
  SELECT id, row_number() OVER (ORDER BY id) AS rn
  FROM people
)
UPDATE people AS p
SET email = CASE
    WHEN p.email IS NULL THEN NULL
    ELSE ('person+' || numbered.rn || '@example.test')::citext
  END,
  phone = '',
  signal = '',
  telegram = '',
  twitter_handle = '',
  nostr = '',
  github_url = '',
  instagram = '',
  linkedin = '',
  website_url = '',
  tshirt = ''
FROM numbered
WHERE p.id = numbered.id;

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
SET name = 'Volunteer ' || numbered.rn,
  email = ('volunteer+' || numbered.rn || '@example.test')::citext,
  phone = '',
  signal = '',
  availability = '{}',
  contact_at = '',
  comments = '',
  discovered_via = '',
  hometown = '',
  twitter_handle = '',
  nostr = '',
  shirt = '',
  captcha = 0
FROM numbered
WHERE v.id = numbered.id;

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
