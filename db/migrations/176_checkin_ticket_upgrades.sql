ALTER TABLE registrations
 ADD COLUMN checkin_requirements jsonb NOT NULL DEFAULT '[]'::jsonb,
 ADD COLUMN proof_checked_at timestamptz,
 ADD COLUMN proof_checked_by text NOT NULL DEFAULT '',
 ADD COLUMN proof_revision bigint NOT NULL DEFAULT 0,
 ADD COLUMN full_ticket_upgraded_at timestamptz;

CREATE FUNCTION ticket_checkin_requirements(kind text, discount uuid) RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT coalesce(jsonb_agg(requirement), '[]'::jsonb) FROM (
   SELECT jsonb_build_object('label','Local ticket','detail','Check local ID.') AS requirement WHERE lower(kind)='local'
   UNION ALL SELECT jsonb_build_object('label','Student ticket','detail','Check student ID.') WHERE lower(kind)='student'
   UNION ALL SELECT jsonb_build_object('label',checkout_label,'detail',checkout_restrictions)
     FROM discounts WHERE id=discount AND checkout_conference_id IS NOT NULL AND btrim(checkout_restrictions)<>''
 ) requirements;
$$;
UPDATE registrations SET checkin_requirements=ticket_checkin_requirements(type,discount_id);
UPDATE registrations SET proof_checked_at=checked_in_at,proof_checked_by='previous check-in'
 WHERE checked_in_at IS NOT NULL AND jsonb_array_length(checkin_requirements)>0;

-- Keep the purchased requirements even if an offer is subsequently edited.
-- Run after the attendee ownership trigger, so transferred tickets need fresh proof.
CREATE FUNCTION snapshot_ticket_checkin_requirements() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
   NEW.checkin_requirements := ticket_checkin_requirements(NEW.type,NEW.discount_id);
 ELSIF NEW.type IS DISTINCT FROM OLD.type OR NEW.discount_id IS DISTINCT FROM OLD.discount_id
       OR NEW.attendee_access_key IS DISTINCT FROM OLD.attendee_access_key THEN
   IF NEW.type IS DISTINCT FROM OLD.type OR NEW.discount_id IS DISTINCT FROM OLD.discount_id THEN
     NEW.checkin_requirements := ticket_checkin_requirements(NEW.type,NEW.discount_id);
   END IF;
   NEW.proof_checked_at := NULL;
   NEW.proof_checked_by := '';
   NEW.proof_revision := OLD.proof_revision+1;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER registrations_snapshot_checkin_requirements BEFORE INSERT OR UPDATE ON registrations
 FOR EACH ROW EXECUTE FUNCTION snapshot_ticket_checkin_requirements();

CREATE TABLE ticket_checkin_upgrades (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ticket_ref text NOT NULL REFERENCES registrations(ref_id) ON DELETE CASCADE,
 owner_key uuid NOT NULL,
 proof_revision bigint NOT NULL,
 original_type text NOT NULL,
 operator text NOT NULL,
 currency text NOT NULL,
 previous_cents bigint NOT NULL CHECK(previous_cents>=0),
 full_price_cents bigint NOT NULL CHECK(full_price_cents>=0),
 amount_cents bigint NOT NULL CHECK(amount_cents>=0),
 status text NOT NULL DEFAULT 'creating' CHECK(status IN ('creating','pending','processing','underpaid','paid','expired','refunded','failed','review')),
 charge_id text UNIQUE,
 total_sats bigint NOT NULL DEFAULT 0 CHECK(total_sats>=0),
 invoice text NOT NULL DEFAULT '',
 chain_address text NOT NULL DEFAULT '',
 expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 checked_at timestamptz,
 paid_at timestamptz
);
CREATE UNIQUE INDEX ticket_checkin_upgrade_active ON ticket_checkin_upgrades(ticket_ref)
 WHERE status IN ('creating','pending','processing','underpaid');
