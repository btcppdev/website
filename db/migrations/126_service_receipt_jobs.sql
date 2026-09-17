CREATE TABLE service_receipt_jobs (
 order_id uuid PRIMARY KEY REFERENCES shop_orders(id) ON DELETE CASCADE,
 recipient text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','cancelled','review')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(),
 accepted_at timestamptz,
 CHECK((state='accepted')=(accepted_at IS NOT NULL))
);
CREATE INDEX service_receipt_jobs_due ON service_receipt_jobs(next_attempt_at,lease_until) WHERE state='pending';

-- Payment adapters write settled_at only after verifying provider evidence.
-- Queueing shares that transaction, including Stripe invoice/order creation.
-- Existing settled orders are intentionally not backfilled into outgoing email.
CREATE FUNCTION enqueue_service_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO service_receipt_jobs(order_id,recipient)
 SELECT o.id,o.buyer_email FROM service_purchase_requests r
 JOIN shop_orders o ON o.id=r.order_id WHERE r.id=NEW.purchase_id
 ON CONFLICT(order_id) DO NOTHING;
 RETURN NEW;
END;
$$;
CREATE TRIGGER enqueue_service_receipt_on_settlement
 AFTER INSERT OR UPDATE OF settled_at ON service_payment_attempts
 FOR EACH ROW WHEN (NEW.settled_at IS NOT NULL)
 EXECUTE FUNCTION enqueue_service_receipt();

CREATE FUNCTION protect_service_receipt_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.order_id IS DISTINCT FROM OLD.order_id OR NEW.recipient IS DISTINCT FROM OLD.recipient
 OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.attempts<OLD.attempts
 OR (OLD.state='accepted' AND (NEW.state IS DISTINCT FROM OLD.state OR NEW.accepted_at IS DISTINCT FROM OLD.accepted_at)) THEN
 RAISE EXCEPTION 'Receipt identity and accepted delivery are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER protect_service_receipt_identity BEFORE UPDATE ON service_receipt_jobs
 FOR EACH ROW EXECUTE FUNCTION protect_service_receipt_identity();
