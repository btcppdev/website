CREATE TABLE alerts_stripe_decrease_attachments (
 quote_id uuid PRIMARY KEY REFERENCES alerts_stripe_decreases(quote_id),
 first_attempt_at timestamptz NOT NULL DEFAULT now(),
 attempts bigint NOT NULL DEFAULT 1 CHECK(attempts>0),
 lease_until timestamptz NOT NULL,
 schedule_id text UNIQUE,
 CHECK(schedule_id IS NULL OR schedule_id LIKE 'sub_sched\_%' ESCAPE '\')
);
CREATE FUNCTION protect_alerts_decrease_attachment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.quote_id IS DISTINCT FROM OLD.quote_id
 OR NEW.first_attempt_at IS DISTINCT FROM OLD.first_attempt_at
 OR NEW.attempts<OLD.attempts
 OR (OLD.schedule_id IS NOT NULL AND NEW.schedule_id IS DISTINCT FROM OLD.schedule_id) THEN
 RAISE EXCEPTION 'Decrease attachment identity and schedule reference are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_decrease_attachment_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_attachments
FOR EACH ROW EXECUTE FUNCTION protect_alerts_decrease_attachment();
