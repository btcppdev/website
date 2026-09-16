CREATE TABLE alerts_stripe_increase_quotes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 checkout_id uuid NOT NULL REFERENCES alerts_stripe_subscriptions(checkout_id),
 revision bigint NOT NULL CHECK(revision>0),
 preview jsonb NOT NULL CHECK(jsonb_typeof(preview)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at),
 UNIQUE(id,checkout_id)
);
CREATE TRIGGER alerts_stripe_increase_quote_protected BEFORE UPDATE OR DELETE ON alerts_stripe_increase_quotes
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();

CREATE TABLE alerts_stripe_increases (
 quote_id uuid PRIMARY KEY,
 checkout_id uuid NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','applied','expired','review')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(quote_id,checkout_id) REFERENCES alerts_stripe_increase_quotes(id,checkout_id)
);
CREATE UNIQUE INDEX alerts_stripe_increase_pending ON alerts_stripe_increases(checkout_id) WHERE state='pending';
CREATE FUNCTION protect_alerts_increase_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.quote_id IS DISTINCT FROM OLD.quote_id OR NEW.checkout_id IS DISTINCT FROM OLD.checkout_id
 OR NEW.created_at IS DISTINCT FROM OLD.created_at OR (OLD.state<>'pending' AND NEW.state IS DISTINCT FROM OLD.state) THEN
 RAISE EXCEPTION 'Increase identity and resolved outcomes cannot be changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_increase_protected BEFORE UPDATE OR DELETE ON alerts_stripe_increases
FOR EACH ROW EXECUTE FUNCTION protect_alerts_increase_identity();
