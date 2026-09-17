CREATE TABLE alerts_stripe_decrease_withdrawals (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 quote_id uuid NOT NULL UNIQUE REFERENCES alerts_stripe_decrease_schedules(quote_id),
 created_at timestamptz NOT NULL DEFAULT now(),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','released','review')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 lease_until timestamptz NOT NULL DEFAULT now(),
 snapshot jsonb CHECK(snapshot IS NULL OR jsonb_typeof(snapshot)='object'),
 CHECK((state='released')=(snapshot IS NOT NULL))
);
CREATE FUNCTION protect_alerts_decrease_withdrawal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.quote_id IS DISTINCT FROM OLD.quote_id
 OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.attempts<OLD.attempts
 OR (OLD.state='released' AND (NEW.state IS DISTINCT FROM OLD.state OR NEW.snapshot IS DISTINCT FROM OLD.snapshot)) THEN
 RAISE EXCEPTION 'Decrease withdrawal identity and release evidence are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_decrease_withdrawal_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_withdrawals
FOR EACH ROW EXECUTE FUNCTION protect_alerts_decrease_withdrawal();
