CREATE TABLE alerts_stripe_increase_submissions (
 quote_id uuid PRIMARY KEY REFERENCES alerts_stripe_increases(quote_id),
 first_attempt_at timestamptz NOT NULL DEFAULT now(),
 attempts bigint NOT NULL DEFAULT 1 CHECK(attempts>0),
 lease_until timestamptz NOT NULL,
 invoice_id text UNIQUE,
 CHECK(invoice_id IS NULL OR invoice_id LIKE 'in\_%' ESCAPE '\')
);
CREATE FUNCTION protect_alerts_increase_submission() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.quote_id IS DISTINCT FROM OLD.quote_id
 OR NEW.first_attempt_at IS DISTINCT FROM OLD.first_attempt_at
 OR NEW.attempts<OLD.attempts
 OR (OLD.invoice_id IS NOT NULL AND NEW.invoice_id IS DISTINCT FROM OLD.invoice_id) THEN
 RAISE EXCEPTION 'Upgrade submission identity and invoice reference are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_increase_submission_protected BEFORE UPDATE OR DELETE ON alerts_stripe_increase_submissions
FOR EACH ROW EXECUTE FUNCTION protect_alerts_increase_submission();
