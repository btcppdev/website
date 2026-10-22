-- Additive transition: existing address columns are compatibility mirrors.
-- The audit command can run before this migration, without changing data.
LOCK TABLE person_emails, subscribers, subscriber_subscriptions IN SHARE ROW EXCLUSIVE MODE;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM person_emails GROUP BY lower(btrim(email::text)) HAVING count(*) > 1)
     OR EXISTS (SELECT 1 FROM subscribers GROUP BY lower(btrim(email::text)) HAVING count(*) > 1)
     OR EXISTS (SELECT 1 FROM person_emails WHERE btrim(email::text) = '')
     OR EXISTS (SELECT 1 FROM subscribers WHERE btrim(email::text) = '') THEN
    RAISE EXCEPTION 'Email directory normalization conflicts: run email-directory-audit and resolve before migration';
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      SELECT DISTINCT ON(lower(btrim(email::text)),newsletter) email,newsletter,subscribed
      FROM newsletter_consent_requests WHERE newsletter<>'insider'
      ORDER BY lower(btrim(email::text)),newsletter,recorded_at DESC,subscribed ASC
    ) latest JOIN subscribers s ON lower(btrim(s.email::text))=lower(btrim(latest.email::text))
    JOIN subscriber_subscriptions ss ON ss.subscriber_id=s.id AND ss.name=latest.newsletter
    WHERE NOT latest.subscribed
  ) THEN
    RAISE EXCEPTION 'Email preference conflicts: active membership disagrees with latest explicit opt-out; run email-directory-audit';
  END IF;
END $$;

CREATE TABLE email_contacts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email citext NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (email::text = lower(btrim(email::text)) AND email::text <> '')
);
COMMENT ON TABLE email_contacts IS 'Authoritative email address identity; independent of accounts and consent. Addresses are immutable; an address change links a different contact.';

INSERT INTO email_contacts(email,created_at)
SELECT lower(btrim(email::text)), min(created_at)
FROM (SELECT email,created_at FROM person_emails UNION ALL SELECT email,created_at FROM subscribers) addresses
GROUP BY lower(btrim(email::text));

ALTER TABLE person_emails ADD COLUMN contact_id uuid REFERENCES email_contacts(id);
ALTER TABLE subscribers ADD COLUMN contact_id uuid REFERENCES email_contacts(id);
UPDATE person_emails p SET contact_id=c.id FROM email_contacts c WHERE c.email=lower(btrim(p.email::text));
UPDATE subscribers s SET contact_id=c.id FROM email_contacts c WHERE c.email=lower(btrim(s.email::text));
ALTER TABLE person_emails ALTER COLUMN contact_id SET NOT NULL;
ALTER TABLE subscribers ALTER COLUMN contact_id SET NOT NULL;
CREATE UNIQUE INDEX person_emails_contact_idx ON person_emails(contact_id);
CREATE UNIQUE INDEX subscribers_contact_idx ON subscribers(contact_id);

CREATE FUNCTION email_contact_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.email IS DISTINCT FROM OLD.email OR NEW.id IS DISTINCT FROM OLD.id THEN
    RAISE EXCEPTION 'Email contact identity is immutable; link a different contact for an address change';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER email_contacts_immutable BEFORE UPDATE ON email_contacts
FOR EACH ROW EXECUTE FUNCTION email_contact_immutable();

CREATE FUNCTION link_email_contact() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE resolved uuid;
BEGIN
  INSERT INTO email_contacts(email) VALUES(lower(btrim(NEW.email::text)))
  ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id INTO resolved;
  IF TG_OP='UPDATE' AND NEW.contact_id IS DISTINCT FROM OLD.contact_id AND NEW.contact_id IS DISTINCT FROM resolved THEN
    RAISE EXCEPTION 'Email and contact ID do not match';
  ELSIF TG_OP='INSERT' AND NEW.contact_id IS NOT NULL AND NEW.contact_id IS DISTINCT FROM resolved THEN
    RAISE EXCEPTION 'Email and contact ID do not match';
  END IF;
  IF TG_TABLE_NAME='subscribers' AND TG_OP='UPDATE' AND OLD.contact_id IS DISTINCT FROM resolved THEN
    RAISE EXCEPTION 'Subscriber address changes require an explicit preference transfer';
  END IF;
  NEW.contact_id := resolved;
  RETURN NEW;
END $$;
CREATE TRIGGER person_emails_link_contact BEFORE INSERT OR UPDATE OF email,contact_id ON person_emails
FOR EACH ROW EXECUTE FUNCTION link_email_contact();
CREATE TRIGGER subscribers_link_contact BEFORE INSERT OR UPDATE OF email,contact_id ON subscribers
FOR EACH ROW EXECUTE FUNCTION link_email_contact();

CREATE TABLE email_memberships (
  contact_id uuid NOT NULL REFERENCES email_contacts(id),
  service text NOT NULL CHECK(service<>''),
  newsletter text NOT NULL CHECK(newsletter<>''),
  status text NOT NULL CHECK(status IN ('subscribed','unsubscribed')),
  source text NOT NULL CHECK(source<>''),
  consented_at timestamptz,
  unsubscribed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  version bigint NOT NULL DEFAULT 1 CHECK(version>0),
  PRIMARY KEY(contact_id,service,newsletter),
  CHECK (NOT(service='btcpp' AND newsletter='insider')),
  CHECK ((status='subscribed' AND unsubscribed_at IS NULL) OR (status='unsubscribed' AND unsubscribed_at IS NOT NULL))
);
CREATE INDEX email_memberships_audience_idx ON email_memberships(service,newsletter,contact_id) WHERE status='subscribed';
CREATE TABLE email_preference_events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  contact_id uuid NOT NULL REFERENCES email_contacts(id),
  service text NOT NULL,
  newsletter text NOT NULL,
  status text NOT NULL CHECK(status IN ('subscribed','unsubscribed')),
  source text NOT NULL,
  membership_version bigint NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(contact_id,service,newsletter,membership_version)
);
CREATE INDEX email_preference_events_contact_idx ON email_preference_events(contact_id,id);

-- No invented consent timestamp: legacy memberships lack that evidence.
INSERT INTO email_memberships(contact_id,service,newsletter,status,source)
SELECT s.contact_id,'btcpp',ss.name,'subscribed','legacy-import'
FROM subscriber_subscriptions ss JOIN subscribers s ON s.id=ss.subscriber_id
WHERE ss.name<>'insider';
INSERT INTO email_preference_events(contact_id,service,newsletter,status,source,membership_version)
SELECT contact_id,service,newsletter,status,source,version FROM email_memberships;

-- Preserve explicit opt-outs from the existing consent log where membership
-- absence alone cannot tell us whether a person ever subscribed.
INSERT INTO email_memberships(contact_id,service,newsletter,status,source,unsubscribed_at)
SELECT DISTINCT ON(c.id,r.newsletter) c.id,'btcpp',r.newsletter,'unsubscribed','legacy-consent-history',r.recorded_at
FROM newsletter_consent_requests r JOIN email_contacts c ON c.email=lower(btrim(r.email::text))
WHERE r.newsletter<>'insider' AND NOT r.subscribed
  AND NOT EXISTS(SELECT 1 FROM email_memberships m WHERE m.contact_id=c.id AND m.service='btcpp' AND m.newsletter=r.newsletter)
ORDER BY c.id,r.newsletter,r.recorded_at DESC;
INSERT INTO email_preference_events(contact_id,service,newsletter,status,source,membership_version)
SELECT contact_id,service,newsletter,status,source,version FROM email_memberships WHERE status='unsubscribed';

ALTER TABLE subscribers ADD CONSTRAINT subscribers_id_contact_unique UNIQUE(id,contact_id);
ALTER TABLE subscriber_subscriptions ADD COLUMN contact_id uuid;
UPDATE subscriber_subscriptions ss SET contact_id=s.contact_id FROM subscribers s WHERE s.id=ss.subscriber_id;
ALTER TABLE subscriber_subscriptions ALTER COLUMN contact_id SET NOT NULL;
ALTER TABLE subscriber_subscriptions ADD CONSTRAINT subscriber_subscriptions_contact_fk
FOREIGN KEY(subscriber_id,contact_id) REFERENCES subscribers(id,contact_id) ON DELETE CASCADE;
CREATE FUNCTION link_membership_contact() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='UPDATE' AND (NEW.subscriber_id IS DISTINCT FROM OLD.subscriber_id OR NEW.name IS DISTINCT FROM OLD.name) THEN
    RAISE EXCEPTION 'Change membership identity by deleting and inserting, preserving preference history';
  END IF;
  SELECT contact_id INTO NEW.contact_id FROM subscribers WHERE id=NEW.subscriber_id;
  RETURN NEW;
END $$;
CREATE TRIGGER subscriber_subscriptions_link_contact BEFORE INSERT OR UPDATE ON subscriber_subscriptions
FOR EACH ROW EXECUTE FUNCTION link_membership_contact();

-- Temporary one-way bridge: legacy writers remain the sole writer of btcpp
-- memberships until the contact-based preference service replaces them.
CREATE FUNCTION mirror_legacy_email_membership() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid; list text; next_status text; changed email_memberships%ROWTYPE;
BEGIN
  IF TG_OP='DELETE' THEN target:=OLD.contact_id; list:=OLD.name; next_status:='unsubscribed';
  ELSE target:=NEW.contact_id; list:=NEW.name; next_status:='subscribed'; END IF;
  IF list='insider' THEN RETURN NULL; END IF;
  INSERT INTO email_memberships(contact_id,service,newsletter,status,source,consented_at,unsubscribed_at)
  VALUES(target,'btcpp',list,next_status,'legacy-subscription',CASE WHEN next_status='subscribed' THEN now() END,CASE WHEN next_status='unsubscribed' THEN now() END)
  ON CONFLICT(contact_id,service,newsletter) DO UPDATE SET
    status=excluded.status,source=excluded.source,
    consented_at=CASE WHEN excluded.status='subscribed' THEN now() ELSE email_memberships.consented_at END,
    unsubscribed_at=excluded.unsubscribed_at,updated_at=now(),version=email_memberships.version+1
  WHERE email_memberships.status<>excluded.status
  RETURNING * INTO changed;
  IF FOUND THEN
    INSERT INTO email_preference_events(contact_id,service,newsletter,status,source,membership_version)
    VALUES(changed.contact_id,changed.service,changed.newsletter,changed.status,changed.source,changed.version);
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER subscriber_subscriptions_directory AFTER INSERT OR DELETE ON subscriber_subscriptions
FOR EACH ROW EXECUTE FUNCTION mirror_legacy_email_membership();
