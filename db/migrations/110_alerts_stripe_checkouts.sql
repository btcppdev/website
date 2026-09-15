-- Checkout intents are account-owned before contacting Stripe. Session IDs are
-- bound once; an ambiguous timeout retains the original idempotency key.
CREATE TABLE alerts_stripe_checkouts (
 id uuid PRIMARY KEY,
 person_id uuid REFERENCES people(id) ON DELETE SET NULL,
 request_key text NOT NULL CHECK(length(request_key) BETWEEN 16 AND 128),
 intent jsonb NOT NULL CHECK(jsonb_typeof(intent)='object'),
 stripe_session_id text UNIQUE CHECK(stripe_session_id LIKE 'cs_%'),
 created_at timestamptz NOT NULL DEFAULT now(),
 closed_at timestamptz,
 UNIQUE(person_id,request_key)
);
CREATE UNIQUE INDEX alerts_stripe_checkout_open_account ON alerts_stripe_checkouts(person_id) WHERE closed_at IS NULL;
CREATE FUNCTION protect_alerts_stripe_checkout() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION 'Stripe checkout evidence cannot be deleted' USING ERRCODE='23514';
 END IF;
 IF NEW.id IS DISTINCT FROM OLD.id OR NEW.request_key IS DISTINCT FROM OLD.request_key
  OR NEW.intent IS DISTINCT FROM OLD.intent OR NEW.created_at IS DISTINCT FROM OLD.created_at
  OR (NEW.person_id IS DISTINCT FROM OLD.person_id AND NEW.person_id IS NOT NULL)
  OR (OLD.stripe_session_id IS NOT NULL AND NEW.stripe_session_id IS DISTINCT FROM OLD.stripe_session_id)
  OR (OLD.closed_at IS NOT NULL AND NEW.closed_at IS DISTINCT FROM OLD.closed_at)
 THEN
  RAISE EXCEPTION 'Stripe checkout identity cannot be rewritten' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_stripe_checkout_protected BEFORE UPDATE OR DELETE ON alerts_stripe_checkouts
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_checkout();
