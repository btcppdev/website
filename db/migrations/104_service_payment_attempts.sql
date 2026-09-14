CREATE TABLE service_payment_attempts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  purchase_id uuid NOT NULL UNIQUE REFERENCES service_purchase_requests(id) ON DELETE RESTRICT,
  provider text NOT NULL CHECK (provider IN ('stripe','opennode')),
  provider_reference text,
  created_at timestamptz NOT NULL DEFAULT now(),
  settled_at timestamptz,
  CHECK (provider_reference IS NULL OR length(provider_reference) BETWEEN 1 AND 255),
  CHECK (settled_at IS NULL OR provider_reference IS NOT NULL),
  UNIQUE(provider,provider_reference)
);

CREATE TABLE service_payment_events (
  provider text NOT NULL,
  event_id text NOT NULL CHECK (length(event_id) BETWEEN 1 AND 255),
  attempt_id uuid NOT NULL REFERENCES service_payment_attempts(id) ON DELETE RESTRICT,
  total_cents integer NOT NULL CHECK (total_cents > 0),
  currency text NOT NULL CHECK (currency = 'USD'),
  paid_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(provider,event_id),
  CHECK (provider IN ('stripe','opennode'))
);

CREATE FUNCTION protect_service_payment_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'service payment attempts cannot be deleted' USING ERRCODE = '23514';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.purchase_id IS DISTINCT FROM OLD.purchase_id
    OR NEW.provider IS DISTINCT FROM OLD.provider OR NEW.created_at IS DISTINCT FROM OLD.created_at
    OR (OLD.provider_reference IS NOT NULL AND NEW.provider_reference IS DISTINCT FROM OLD.provider_reference)
    OR (OLD.settled_at IS NOT NULL AND NEW.settled_at IS DISTINCT FROM OLD.settled_at)
  THEN
    RAISE EXCEPTION 'service payment identity and settlement cannot be rewritten' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER service_payment_attempt_protected BEFORE UPDATE OR DELETE ON service_payment_attempts
FOR EACH ROW EXECUTE FUNCTION protect_service_payment_attempt();

CREATE FUNCTION protect_service_payment_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'service payment events are immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER service_payment_event_immutable BEFORE UPDATE OR DELETE ON service_payment_events
FOR EACH ROW EXECUTE FUNCTION protect_service_payment_event();
