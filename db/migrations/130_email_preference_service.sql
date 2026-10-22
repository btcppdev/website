ALTER TABLE email_memberships ADD COLUMN delivery_mode text NOT NULL DEFAULT 'as_published' CHECK(delivery_mode IN ('as_published','weekly_digest'));
ALTER TABLE email_memberships ADD COLUMN paused_until timestamptz;
CREATE TABLE email_preference_requests (
 service text NOT NULL, request_key text NOT NULL, request_hash bytea NOT NULL,
 contact_id uuid NOT NULL REFERENCES email_contacts(id), recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(service,request_key)
);
CREATE TABLE email_management_tokens (
 token_hash bytea PRIMARY KEY, contact_id uuid NOT NULL REFERENCES email_contacts(id),
 purpose text NOT NULL CHECK(purpose IN ('link','session')), expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_management_expiry_idx ON email_management_tokens(expires_at);
CREATE TABLE email_management_rate_limits (bucket text PRIMARY KEY, window_start timestamptz NOT NULL, attempts int NOT NULL);
-- Browser and service changes use one event source, including legacy bridged writes.
DROP TRIGGER subscriber_subscriptions_directory ON subscriber_subscriptions;
CREATE OR REPLACE FUNCTION mirror_legacy_email_membership() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid; list text; next_status text;
BEGIN
 IF TG_OP='DELETE' THEN target:=OLD.contact_id;list:=OLD.name;next_status:='unsubscribed';
 ELSE target:=NEW.contact_id;list:=NEW.name;next_status:='subscribed'; END IF;
 IF list='insider' OR current_setting('btcpp.directory_write',true)='yes' THEN RETURN NULL; END IF;
 INSERT INTO email_memberships(contact_id,service,newsletter,status,source,consented_at,unsubscribed_at)
 VALUES(target,'btcpp',list,next_status,'legacy-subscription',CASE WHEN next_status='subscribed' THEN now() END,CASE WHEN next_status='unsubscribed' THEN now() END)
 ON CONFLICT(contact_id,service,newsletter) DO UPDATE SET status=excluded.status,source=excluded.source,
 consented_at=CASE WHEN excluded.status='subscribed' THEN now() ELSE email_memberships.consented_at END,
 unsubscribed_at=excluded.unsubscribed_at,updated_at=now(),version=email_memberships.version+1
 WHERE email_memberships.status<>excluded.status;
 RETURN NULL;
END $$;
CREATE TRIGGER subscriber_subscriptions_directory AFTER INSERT OR DELETE ON subscriber_subscriptions
FOR EACH ROW EXECUTE FUNCTION mirror_legacy_email_membership();
CREATE TABLE email_account_proofs (
 service text NOT NULL, proof_id text NOT NULL, contact_id uuid NOT NULL REFERENCES email_contacts(id),
 person_id uuid REFERENCES people(id) ON DELETE SET NULL, verified_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(service,proof_id)
);
CREATE TABLE email_preference_cancellations (
 event_id bigint PRIMARY KEY REFERENCES email_preference_events(id),
 contact_id uuid NOT NULL REFERENCES email_contacts(id), newsletter text NOT NULL,
 recipient text NOT NULL DEFAULT '',
 available_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz
);
CREATE INDEX email_management_rate_expiry_idx ON email_management_rate_limits(window_start);
CREATE INDEX email_cancellations_due_idx ON email_preference_cancellations(available_at,event_id) WHERE completed_at IS NULL;
ALTER TABLE email_contacts ADD COLUMN erased_at timestamptz;
CREATE OR REPLACE FUNCTION email_contact_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id IS DISTINCT FROM OLD.id OR (NEW.email IS DISTINCT FROM OLD.email AND NOT(coalesce(current_setting('btcpp.contact_erasure',true),'')='yes' AND NEW.erased_at IS NOT NULL)) THEN
 RAISE EXCEPTION 'Email contact identity is immutable; link a different contact for an address change';
 END IF;
 RETURN NEW;
END $$;
-- Queue snapshots exist only until cancellation succeeds, including account erasure.

CREATE FUNCTION record_email_preference_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE event bigint;
BEGIN
 INSERT INTO email_preference_events(contact_id,service,newsletter,status,source,membership_version)
 VALUES(NEW.contact_id,NEW.service,NEW.newsletter,NEW.status,NEW.source,NEW.version) RETURNING id INTO event;
 IF NEW.service='btcpp' AND NEW.status='unsubscribed' THEN
 INSERT INTO email_preference_cancellations(event_id,contact_id,newsletter,recipient)
 SELECT event,NEW.contact_id,NEW.newsletter,email::text FROM email_contacts WHERE id=NEW.contact_id;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER email_membership_event AFTER INSERT OR UPDATE ON email_memberships
FOR EACH ROW EXECUTE FUNCTION record_email_preference_event();
