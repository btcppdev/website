CREATE TABLE alerts_stripe_decrease_quotes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 checkout_id uuid NOT NULL REFERENCES alerts_stripe_subscriptions(checkout_id),
 revision bigint NOT NULL CHECK(revision>0),
 preview jsonb NOT NULL CHECK(jsonb_typeof(preview)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at),
 UNIQUE(id,checkout_id)
);
CREATE TRIGGER alerts_stripe_decrease_quote_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_quotes
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();
CREATE TABLE alerts_stripe_decreases (
 quote_id uuid PRIMARY KEY,
 checkout_id uuid NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','scheduled','applied','expired','review','cancelled')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(quote_id,checkout_id) REFERENCES alerts_stripe_decrease_quotes(id,checkout_id)
);
CREATE UNIQUE INDEX alerts_stripe_decrease_unresolved ON alerts_stripe_decreases(checkout_id) WHERE state IN ('pending','scheduled','review');
CREATE FUNCTION protect_alerts_decrease_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.quote_id IS DISTINCT FROM OLD.quote_id OR NEW.checkout_id IS DISTINCT FROM OLD.checkout_id
 OR NEW.created_at IS DISTINCT FROM OLD.created_at
 OR (OLD.state IN ('applied','expired','cancelled') AND NEW.state IS DISTINCT FROM OLD.state) THEN
 RAISE EXCEPTION 'Decrease identity and resolved outcomes cannot be changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_decrease_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decreases
FOR EACH ROW EXECUTE FUNCTION protect_alerts_decrease_identity();
