BEGIN;

CREATE TABLE person_merge_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  canonical_person_id uuid NOT NULL REFERENCES people(id) ON DELETE RESTRICT,
  source_person_id uuid NOT NULL,
  merged_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  status text NOT NULL DEFAULT 'merged',
  canonical_snapshot jsonb NOT NULL,
  source_snapshot jsonb NOT NULL,
  decisions jsonb NOT NULL DEFAULT '{}'::jsonb,
  relationship_manifest jsonb NOT NULL DEFAULT '{}'::jsonb,
  undo_expires_at timestamptz NOT NULL,
  reverted_at timestamptz,
  reverted_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  restore_warning jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (canonical_person_id <> source_person_id),
  CHECK (status IN ('merged', 'reverted')),
  CHECK ((status = 'merged' AND reverted_at IS NULL) OR
         (status = 'reverted' AND reverted_at IS NOT NULL))
);

CREATE INDEX person_merge_events_canonical_idx
ON person_merge_events (canonical_person_id, created_at DESC);

CREATE INDEX person_merge_events_source_idx
ON person_merge_events (source_person_id, created_at DESC);

CREATE TABLE person_emails (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  email citext NOT NULL UNIQUE,
  is_primary boolean NOT NULL DEFAULT false,
  verified_at timestamptz NOT NULL DEFAULT now(),
  origin_merge_event_id uuid REFERENCES person_merge_events(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (email <> '')
);

CREATE UNIQUE INDEX person_emails_one_primary_idx
ON person_emails (person_id)
WHERE is_primary;

CREATE INDEX person_emails_person_idx
ON person_emails (person_id, is_primary DESC, created_at);

CREATE TRIGGER person_emails_set_updated_at
BEFORE UPDATE ON person_emails
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE person_email_conflicts (
  email citext NOT NULL,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  detected_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (email, person_id),
  CHECK (email <> '')
);

CREATE INDEX person_email_conflicts_person_idx
ON person_email_conflicts (person_id);

CREATE TABLE person_email_verifications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  email citext NOT NULL,
  token_hash bytea NOT NULL UNIQUE,
  make_primary boolean NOT NULL DEFAULT false,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (email <> ''),
  CHECK (expires_at > created_at)
);

CREATE INDEX person_email_verifications_person_idx
ON person_email_verifications (person_id, created_at DESC);

WITH email_counts AS (
  SELECT id, email, count(*) OVER (PARTITION BY email) AS matches
  FROM people
  WHERE email IS NOT NULL AND email <> ''
)
INSERT INTO person_emails (person_id, email, is_primary, verified_at)
SELECT id, email, true, now()
FROM email_counts
WHERE matches = 1;

WITH email_counts AS (
  SELECT id, email, count(*) OVER (PARTITION BY email) AS matches
  FROM people
  WHERE email IS NOT NULL AND email <> ''
)
INSERT INTO person_email_conflicts (email, person_id)
SELECT email, id
FROM email_counts
WHERE matches > 1;

ALTER TABLE registrations
ADD COLUMN person_id uuid REFERENCES people(id) ON DELETE SET NULL;

CREATE INDEX registrations_person_idx
ON registrations (person_id, registered_at DESC);

ALTER TABLE volunteers
ADD COLUMN person_id uuid REFERENCES people(id) ON DELETE SET NULL;

CREATE INDEX volunteers_person_idx
ON volunteers (person_id, created_at DESC);

ALTER TABLE shop_orders
ADD COLUMN buyer_person_id uuid REFERENCES people(id) ON DELETE SET NULL;

CREATE INDEX shop_orders_buyer_person_idx
ON shop_orders (buyer_person_id, created_at DESC);

ALTER TABLE sponsor_ticket_grants
ADD COLUMN recipient_person_id uuid REFERENCES people(id) ON DELETE SET NULL;

CREATE INDEX sponsor_ticket_grants_recipient_person_idx
ON sponsor_ticket_grants (recipient_person_id, created_at DESC);

ALTER TABLE discounts
ADD COLUMN affiliate_person_id uuid REFERENCES people(id) ON DELETE SET NULL;

CREATE INDEX discounts_affiliate_person_idx
ON discounts (affiliate_person_id)
WHERE affiliate_person_id IS NOT NULL;

ALTER TABLE affiliate_usages
ADD COLUMN affiliate_person_id uuid REFERENCES people(id) ON DELETE SET NULL;

CREATE INDEX affiliate_usages_affiliate_person_idx
ON affiliate_usages (affiliate_person_id, created_at DESC)
WHERE affiliate_person_id IS NOT NULL;

UPDATE registrations record
SET person_id = email.person_id
FROM person_emails email
WHERE record.person_id IS NULL AND record.email = email.email;

UPDATE volunteers record
SET person_id = email.person_id
FROM person_emails email
WHERE record.person_id IS NULL AND record.email = email.email;

UPDATE shop_orders record
SET buyer_person_id = email.person_id
FROM person_emails email
WHERE record.buyer_person_id IS NULL AND record.buyer_email = email.email;

UPDATE sponsor_ticket_grants record
SET recipient_person_id = email.person_id
FROM person_emails email
WHERE record.recipient_person_id IS NULL AND record.recipient_email = email.email;

UPDATE discounts record
SET affiliate_person_id = email.person_id
FROM person_emails email
WHERE record.affiliate_person_id IS NULL AND record.affiliate_email = email.email;

UPDATE affiliate_usages record
SET affiliate_person_id = email.person_id
FROM person_emails email
WHERE record.affiliate_person_id IS NULL AND record.affiliate_email = email.email;

COMMIT;
