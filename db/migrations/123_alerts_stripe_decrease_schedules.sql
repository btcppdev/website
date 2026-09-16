ALTER TABLE alerts_stripe_decrease_attachments ADD UNIQUE(quote_id,schedule_id);
CREATE TABLE alerts_stripe_decrease_schedules (
 quote_id uuid PRIMARY KEY,
 schedule_id text NOT NULL UNIQUE,
 attempts bigint NOT NULL DEFAULT 1 CHECK(attempts>0),
 lease_until timestamptz NOT NULL,
 snapshot jsonb CHECK(snapshot IS NULL OR jsonb_typeof(snapshot)='object'),
 verified_at timestamptz,
 CHECK((snapshot IS NULL)=(verified_at IS NULL)),
 FOREIGN KEY(quote_id,schedule_id) REFERENCES alerts_stripe_decrease_attachments(quote_id,schedule_id)
);
CREATE FUNCTION protect_alerts_decrease_schedule() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.quote_id IS DISTINCT FROM OLD.quote_id OR NEW.schedule_id IS DISTINCT FROM OLD.schedule_id
 OR NEW.attempts<OLD.attempts
 OR (OLD.snapshot IS NOT NULL AND (NEW.snapshot IS DISTINCT FROM OLD.snapshot OR NEW.verified_at IS DISTINCT FROM OLD.verified_at)) THEN
 RAISE EXCEPTION 'Decrease schedule identity and verified evidence are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_decrease_schedule_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_schedules
FOR EACH ROW EXECUTE FUNCTION protect_alerts_decrease_schedule();
