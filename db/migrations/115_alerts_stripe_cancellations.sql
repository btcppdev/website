CREATE TABLE alerts_stripe_cancellations (
 id uuid PRIMARY KEY,
 checkout_id uuid NOT NULL REFERENCES alerts_stripe_subscriptions(checkout_id),
 period_end bigint NOT NULL CHECK(period_end>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','confirmed','review')),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX alerts_stripe_cancellation_pending ON alerts_stripe_cancellations(checkout_id) WHERE state='pending';
CREATE FUNCTION protect_alerts_cancellation_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.checkout_id IS DISTINCT FROM OLD.checkout_id
 OR NEW.period_end IS DISTINCT FROM OLD.period_end OR NEW.created_at IS DISTINCT FROM OLD.created_at
 OR (OLD.state<>'pending' AND NEW.state IS DISTINCT FROM OLD.state) THEN
 RAISE EXCEPTION 'Cancellation identity and resolved outcomes cannot be changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_cancellation_protected BEFORE UPDATE OR DELETE ON alerts_stripe_cancellations
FOR EACH ROW EXECUTE FUNCTION protect_alerts_cancellation_identity();
