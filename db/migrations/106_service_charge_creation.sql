-- OpenNode order_id is a merchant reference, not a documented idempotency key.
-- Persist a one-time creation claim before any outbound request. An ambiguous
-- failure must be reconciled, never automatically retried as a new charge.
CREATE TABLE service_charge_creation_claims (
 attempt_id uuid PRIMARY KEY REFERENCES service_payment_attempts(id) ON DELETE RESTRICT,
 claimed_at timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION protect_service_charge_creation_claim() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'service charge creation claims are immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER service_charge_creation_claim_immutable
BEFORE UPDATE OR DELETE ON service_charge_creation_claims
FOR EACH ROW EXECUTE FUNCTION protect_service_charge_creation_claim();
