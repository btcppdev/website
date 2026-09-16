CREATE TABLE alerts_stripe_renewals (
 invoice_id text PRIMARY KEY CHECK(invoice_id LIKE 'in_%'),
 checkout_id uuid NOT NULL REFERENCES alerts_stripe_subscriptions(checkout_id),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 state text NOT NULL CHECK(state IN ('failed','recovered','cancelled','paused','expired','refunded','disputed')),
 due_at timestamptz NOT NULL,
 period_end timestamptz NOT NULL CHECK(period_end>due_at),
 paid_slots bigint NOT NULL CHECK(paid_slots>0),
 observed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alerts_stripe_renewals_checkout ON alerts_stripe_renewals(checkout_id);
CREATE FUNCTION protect_alerts_renewal_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.invoice_id IS DISTINCT FROM OLD.invoice_id OR NEW.checkout_id IS DISTINCT FROM OLD.checkout_id
 OR NEW.due_at IS DISTINCT FROM OLD.due_at OR NEW.period_end IS DISTINCT FROM OLD.period_end OR NEW.paid_slots IS DISTINCT FROM OLD.paid_slots
 OR NEW.revision<OLD.revision OR (OLD.state<>'failed' AND NEW.state='failed') THEN
 RAISE EXCEPTION 'Renewal identity or resolved state cannot be reset' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_renewal_protected BEFORE UPDATE OR DELETE ON alerts_stripe_renewals
FOR EACH ROW EXECUTE FUNCTION protect_alerts_renewal_identity();
