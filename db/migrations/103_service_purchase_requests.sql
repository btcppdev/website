-- Immutable purchase evidence survives account removal; mutable shop order
-- status remains available to payment and refund processing.
CREATE TABLE service_purchase_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  request_key text NOT NULL CHECK (length(request_key) BETWEEN 16 AND 128),
  order_id uuid NOT NULL UNIQUE REFERENCES shop_orders(id) ON DELETE RESTRICT,
  quote_snapshot jsonb NOT NULL CHECK (jsonb_typeof(quote_snapshot) = 'object'),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (person_id, request_key)
);

CREATE FUNCTION protect_service_purchase_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'service purchase snapshots cannot be deleted' USING ERRCODE = '23514';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id
    OR NEW.request_key IS DISTINCT FROM OLD.request_key
    OR NEW.order_id IS DISTINCT FROM OLD.order_id
    OR NEW.quote_snapshot IS DISTINCT FROM OLD.quote_snapshot
    OR NEW.created_at IS DISTINCT FROM OLD.created_at
    OR (NEW.person_id IS DISTINCT FROM OLD.person_id AND NEW.person_id IS NOT NULL)
  THEN
    RAISE EXCEPTION 'service purchase snapshots are immutable' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER service_purchase_snapshot_immutable
BEFORE UPDATE OR DELETE ON service_purchase_requests
FOR EACH ROW EXECUTE FUNCTION protect_service_purchase_snapshot();
