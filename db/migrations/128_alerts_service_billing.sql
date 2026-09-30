-- Bitcoin++ Alerts: newsletter consent, service commerce, Stripe lifecycle, and receipts.
-- Consolidated before deployment; applied atomically by the migration runner.

-- newsletter consent api
CREATE TABLE newsletter_consent_requests (
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  request_key text NOT NULL,
  email citext NOT NULL,
  newsletter text NOT NULL CHECK(newsletter IN ('newsletter','insider')),
  subscribed boolean NOT NULL,
  source_client text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(person_id,request_key),
  CHECK(length(request_key) BETWEEN 16 AND 128)
);

-- service order items
-- Shared commerce representation only. Provider settlement and product-specific
-- entitlement fulfillment must be connected before exposing service checkout.
ALTER TABLE shop_orders DROP CONSTRAINT shop_orders_checkout_kind_check;
ALTER TABLE shop_orders ADD CONSTRAINT shop_orders_checkout_kind_check
  CHECK (checkout_kind IN ('merch', 'ticket', 'mixed', 'service', 'digital'));

ALTER TABLE shop_order_items DROP CONSTRAINT shop_order_items_fulfillment_method_check;
ALTER TABLE shop_order_items ADD CONSTRAINT shop_order_items_fulfillment_method_check
  CHECK (fulfillment_method IN ('ship', 'event_pickup', 'pos_takeaway', 'service', 'digital'));

ALTER TABLE shop_order_items
  ADD COLUMN service_product_code text NOT NULL DEFAULT '',
  ADD COLUMN service_resource_id text NOT NULL DEFAULT '',
  ADD COLUMN service_period_start timestamptz,
  ADD COLUMN service_period_end timestamptz,
  ADD CONSTRAINT shop_order_items_service_shape CHECK (
    CASE WHEN fulfillment_method IN ('service', 'digital') THEN
      btrim(service_product_code) <> ''
      AND product_id IS NULL AND variant_id IS NULL
      AND sale_conference_id IS NULL AND pickup_conference_id IS NULL
      AND tax_amount_cents <= line_total_cents
      AND CASE WHEN fulfillment_method = 'service' THEN
        service_period_start IS NOT NULL AND service_period_end IS NOT NULL
        AND service_period_end > service_period_start
      ELSE
        btrim(service_resource_id) <> ''
        AND service_period_start IS NULL AND service_period_end IS NULL
      END
    ELSE
      service_product_code = '' AND service_resource_id = ''
      AND service_period_start IS NULL AND service_period_end IS NULL
    END
  );

COMMENT ON COLUMN shop_order_items.service_product_code IS
  'Server-selected product, e.g. alerts.repository-slots, insider.membership, insider.article.';
COMMENT ON COLUMN shop_order_items.service_resource_id IS
  'Product-scoped resource identifier; required for one-time digital access.';
COMMENT ON COLUMN shop_order_items.tax_amount_cents IS
  'For service/digital items tax is included within line_total_cents, not added to it.';

-- service purchase requests
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

-- service payment attempts
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

-- Verified provider refunds are separate immutable facts from payment settlement.
CREATE TABLE service_payment_refunds (
 provider text NOT NULL CHECK(provider IN ('stripe','opennode')),
 refund_reference text NOT NULL CHECK(length(refund_reference) BETWEEN 1 AND 255),
 attempt_id uuid NOT NULL REFERENCES service_payment_attempts(id) ON DELETE RESTRICT,
 payment_reference text NOT NULL CHECK(length(payment_reference) BETWEEN 1 AND 255),
 amount_cents bigint NOT NULL CHECK(amount_cents>0),
 currency text NOT NULL CHECK(currency='USD'),
 refunded_at timestamptz NOT NULL,
 reason text NOT NULL CHECK(length(reason)<=1000),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(provider,refund_reference)
);
CREATE INDEX service_payment_refunds_attempt ON service_payment_refunds(attempt_id);
CREATE TRIGGER service_payment_refund_immutable BEFORE UPDATE OR DELETE ON service_payment_refunds
FOR EACH ROW EXECUTE FUNCTION protect_service_payment_event();

-- service entitlements
CREATE TABLE service_entitlements (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  purchase_id uuid NOT NULL UNIQUE REFERENCES service_purchase_requests(id) ON DELETE RESTRICT,
  person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  product_code text NOT NULL CHECK (product_code IN ('alerts.repository-slots','insider.membership','insider.article')),
  resource_id text NOT NULL DEFAULT '',
  quantity integer NOT NULL CHECK (quantity>0),
  starts_at timestamptz,
  ends_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  revocation_actor text NOT NULL DEFAULT '',
  revocation_reason text NOT NULL DEFAULT '',
  CHECK ((product_code='insider.article' AND resource_id<>'' AND quantity=1 AND starts_at IS NULL AND ends_at IS NULL)
    OR (product_code<>'insider.article' AND starts_at IS NOT NULL AND ends_at IS NOT NULL AND ends_at>starts_at)),
  CHECK ((revoked_at IS NULL AND revocation_actor='' AND revocation_reason='')
    OR (revoked_at IS NOT NULL AND btrim(revocation_actor)<>'' AND btrim(revocation_reason)<>''))
);
CREATE INDEX service_entitlements_person_idx ON service_entitlements(person_id) WHERE revoked_at IS NULL;

CREATE FUNCTION protect_service_entitlement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'service entitlement evidence cannot be deleted' USING ERRCODE='23514';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.purchase_id IS DISTINCT FROM OLD.purchase_id
    OR NEW.product_code IS DISTINCT FROM OLD.product_code OR NEW.resource_id IS DISTINCT FROM OLD.resource_id
    OR NEW.quantity IS DISTINCT FROM OLD.quantity OR NEW.starts_at IS DISTINCT FROM OLD.starts_at
    OR NEW.ends_at IS DISTINCT FROM OLD.ends_at OR NEW.created_at IS DISTINCT FROM OLD.created_at
    OR (NEW.person_id IS DISTINCT FROM OLD.person_id AND NEW.person_id IS NOT NULL)
    OR (OLD.revoked_at IS NOT NULL AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
      OR NEW.revocation_actor IS DISTINCT FROM OLD.revocation_actor OR NEW.revocation_reason IS DISTINCT FROM OLD.revocation_reason))
  THEN
    RAISE EXCEPTION 'service entitlement cannot be rewritten or reactivated' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER service_entitlement_protected BEFORE UPDATE OR DELETE ON service_entitlements
FOR EACH ROW EXECUTE FUNCTION protect_service_entitlement();

-- service charge creation
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

-- service payment time source
-- OpenNode charge lookup establishes paid status, not a documented payment time.
-- Distinguish the first verified observation from provider-supplied timestamps.
ALTER TABLE service_payment_events ADD COLUMN time_source text NOT NULL DEFAULT 'provider'
  CHECK (time_source IN ('provider','first_verified_observation'));

-- service reconciliation schedule
ALTER TABLE service_payment_attempts ADD COLUMN next_reconcile_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX service_payment_reconcile_due ON service_payment_attempts(next_reconcile_at,id)
WHERE provider='opennode' AND provider_reference IS NOT NULL;

-- service checkout quotes
-- Server-generated pricing and tax evidence reviewed before order confirmation.
-- The caller submits only this opaque identifier, never a price or tax snapshot.
CREATE TABLE service_checkout_quotes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 person_id uuid REFERENCES people(id) ON DELETE SET NULL,
 quote_snapshot jsonb NOT NULL CHECK (jsonb_typeof(quote_snapshot)='object'),
 tax_snapshot jsonb NOT NULL CHECK (jsonb_typeof(tax_snapshot)='object'),
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX service_checkout_quotes_person ON service_checkout_quotes(person_id,created_at);
CREATE FUNCTION protect_service_checkout_quote() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id IS DISTINCT FROM OLD.id OR NEW.quote_snapshot IS DISTINCT FROM OLD.quote_snapshot
  OR NEW.tax_snapshot IS DISTINCT FROM OLD.tax_snapshot OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
  OR NEW.created_at IS DISTINCT FROM OLD.created_at
  OR (NEW.person_id IS DISTINCT FROM OLD.person_id AND NEW.person_id IS NOT NULL)
 THEN
  RAISE EXCEPTION 'checkout quotes cannot be rewritten or reassigned' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER service_checkout_quote_immutable BEFORE UPDATE ON service_checkout_quotes
FOR EACH ROW EXECUTE FUNCTION protect_service_checkout_quote();

-- alerts stripe checkouts
-- Checkout intents are account-owned before contacting Stripe. Session IDs are
-- bound once; an ambiguous timeout retains the original idempotency key.
CREATE TABLE service_stripe_checkouts (
 id uuid PRIMARY KEY,
 product_code text NOT NULL DEFAULT 'alerts.repository-slots'
 CHECK(product_code IN ('alerts.repository-slots','insider.membership.monthly','insider.membership.annual')),
 product_family text GENERATED ALWAYS AS (CASE WHEN product_code='alerts.repository-slots' THEN 'alerts' ELSE 'insider' END) STORED,
 person_id uuid REFERENCES people(id) ON DELETE SET NULL,
 request_key text NOT NULL CHECK(length(request_key) BETWEEN 16 AND 128),
 intent jsonb NOT NULL CHECK(jsonb_typeof(intent)='object'),
 stripe_session_id text UNIQUE CHECK(stripe_session_id LIKE 'cs_%'),
 created_at timestamptz NOT NULL DEFAULT now(),
 closed_at timestamptz,
 UNIQUE(person_id,product_family,request_key)
);
CREATE UNIQUE INDEX alerts_stripe_checkout_open_account ON service_stripe_checkouts(person_id,product_family) WHERE closed_at IS NULL;
CREATE FUNCTION protect_alerts_stripe_checkout() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION 'Stripe checkout evidence cannot be deleted' USING ERRCODE='23514';
 END IF;
 IF NEW.product_code IS DISTINCT FROM OLD.product_code
  OR NEW.id IS DISTINCT FROM OLD.id OR NEW.request_key IS DISTINCT FROM OLD.request_key
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
CREATE TRIGGER alerts_stripe_checkout_protected BEFORE UPDATE OR DELETE ON service_stripe_checkouts
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_checkout();

-- alerts stripe subscriptions
CREATE TABLE service_stripe_subscriptions (
 checkout_id uuid PRIMARY KEY REFERENCES service_stripe_checkouts(id),
 subscription_id text NOT NULL UNIQUE,
 binding jsonb NOT NULL CHECK(jsonb_typeof(binding)='object'),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE service_stripe_invoices (
 invoice_id text PRIMARY KEY,
 checkout_id uuid NOT NULL REFERENCES service_stripe_subscriptions(checkout_id),
 order_id uuid NOT NULL UNIQUE REFERENCES shop_orders(id),
 paid_period jsonb NOT NULL CHECK(jsonb_typeof(paid_period)='object'),
 starts_at timestamptz NOT NULL,
 ends_at timestamptz NOT NULL CHECK(ends_at>starts_at),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alerts_stripe_invoice_periods ON service_stripe_invoices(checkout_id,starts_at,ends_at);
CREATE FUNCTION protect_alerts_stripe_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'Stripe subscription and invoice evidence is immutable' USING ERRCODE='23514';
END;
$$;
CREATE TRIGGER alerts_stripe_subscription_protected BEFORE UPDATE OR DELETE ON service_stripe_subscriptions
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();
CREATE TRIGGER alerts_stripe_invoice_protected BEFORE UPDATE OR DELETE ON service_stripe_invoices
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();

-- alerts stripe recovery schedule
ALTER TABLE service_stripe_checkouts ADD COLUMN next_reconcile_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX alerts_stripe_checkout_recovery_due ON service_stripe_checkouts(next_reconcile_at,id)
 WHERE closed_at IS NULL AND person_id IS NOT NULL AND stripe_session_id IS NOT NULL;

-- alerts stripe subscription state
CREATE TABLE service_stripe_subscription_state (
 checkout_id uuid PRIMARY KEY REFERENCES service_stripe_subscriptions(checkout_id),
 revision bigint NOT NULL CHECK(revision>0),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 observed_at timestamptz NOT NULL DEFAULT now()
);

-- alerts stripe renewals
CREATE TABLE alerts_stripe_renewals (
 invoice_id text PRIMARY KEY CHECK(invoice_id LIKE 'in_%'),
 checkout_id uuid NOT NULL REFERENCES service_stripe_subscriptions(checkout_id),
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

-- alerts stripe cancellations
CREATE TABLE service_stripe_cancellations (
 id uuid PRIMARY KEY,
 checkout_id uuid NOT NULL REFERENCES service_stripe_subscriptions(checkout_id),
 period_end bigint NOT NULL CHECK(period_end>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','confirmed','review')),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX alerts_stripe_cancellation_pending ON service_stripe_cancellations(checkout_id) WHERE state='pending';
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
CREATE TRIGGER alerts_cancellation_protected BEFORE UPDATE OR DELETE ON service_stripe_cancellations
FOR EACH ROW EXECUTE FUNCTION protect_alerts_cancellation_identity();

-- alerts stripe increases
CREATE TABLE alerts_stripe_increase_quotes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 checkout_id uuid NOT NULL REFERENCES service_stripe_subscriptions(checkout_id),
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
 legal_acceptance jsonb CHECK(jsonb_typeof(legal_acceptance)='object'),
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
 OR NEW.legal_acceptance IS DISTINCT FROM OLD.legal_acceptance
 OR NEW.created_at IS DISTINCT FROM OLD.created_at OR (OLD.state<>'pending' AND NEW.state IS DISTINCT FROM OLD.state) THEN
 RAISE EXCEPTION 'Increase identity and resolved outcomes cannot be changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_increase_protected BEFORE UPDATE OR DELETE ON alerts_stripe_increases
FOR EACH ROW EXECUTE FUNCTION protect_alerts_increase_identity();

-- alerts stripe increase payments
CREATE TABLE alerts_stripe_increase_payments (
 quote_id uuid PRIMARY KEY REFERENCES alerts_stripe_increases(quote_id),
 checkout_id uuid NOT NULL,
 invoice_id text NOT NULL UNIQUE,
 order_id uuid NOT NULL UNIQUE REFERENCES shop_orders(id),
 paid_increase jsonb NOT NULL CHECK(jsonb_typeof(paid_increase)='object'),
 binding jsonb NOT NULL CHECK(jsonb_typeof(binding)='object'),
 sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(quote_id,checkout_id) REFERENCES alerts_stripe_increase_quotes(id,checkout_id)
);
CREATE INDEX alerts_stripe_increase_payment_history ON alerts_stripe_increase_payments(checkout_id,sequence DESC);
CREATE TRIGGER alerts_stripe_increase_payment_protected BEFORE UPDATE OR DELETE ON alerts_stripe_increase_payments
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();

-- alerts stripe current bindings
-- Subscription identity is immutable. Paid quantity changes append evidence;
-- operational readers use its latest binding while original checkout replay
-- continues comparing against service_stripe_subscriptions itself.
CREATE VIEW alerts_stripe_current_subscriptions AS
SELECT s.checkout_id,s.subscription_id,s.created_at,
 COALESCE((SELECT p.binding FROM alerts_stripe_increase_payments p
           WHERE p.checkout_id=s.checkout_id ORDER BY p.sequence DESC LIMIT 1),s.binding) AS binding
FROM service_stripe_subscriptions s;

-- alerts stripe increase submissions
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

-- alerts stripe increase expirations
CREATE TABLE alerts_stripe_increase_expirations (
 quote_id uuid PRIMARY KEY REFERENCES alerts_stripe_increases(quote_id),
 invoice_id text NOT NULL UNIQUE,
 subscription_state jsonb NOT NULL CHECK(jsonb_typeof(subscription_state)='object'),
 observed_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER alerts_stripe_increase_expiration_protected BEFORE UPDATE OR DELETE ON alerts_stripe_increase_expirations
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();

-- alerts stripe decreases
CREATE TABLE alerts_stripe_decrease_quotes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 checkout_id uuid NOT NULL REFERENCES service_stripe_subscriptions(checkout_id),
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

-- alerts stripe decrease attachments
CREATE TABLE alerts_stripe_decrease_attachments (
 quote_id uuid PRIMARY KEY REFERENCES alerts_stripe_decreases(quote_id),
 first_attempt_at timestamptz NOT NULL DEFAULT now(),
 attempts bigint NOT NULL DEFAULT 1 CHECK(attempts>0),
 lease_until timestamptz NOT NULL,
 schedule_id text UNIQUE,
 CHECK(schedule_id IS NULL OR schedule_id LIKE 'sub_sched\_%' ESCAPE '\')
);
CREATE FUNCTION protect_alerts_decrease_attachment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.quote_id IS DISTINCT FROM OLD.quote_id
 OR NEW.first_attempt_at IS DISTINCT FROM OLD.first_attempt_at
 OR NEW.attempts<OLD.attempts
 OR (OLD.schedule_id IS NOT NULL AND NEW.schedule_id IS DISTINCT FROM OLD.schedule_id) THEN
 RAISE EXCEPTION 'Decrease attachment identity and schedule reference are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_decrease_attachment_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_attachments
FOR EACH ROW EXECUTE FUNCTION protect_alerts_decrease_attachment();

-- alerts stripe decrease schedules
ALTER TABLE alerts_stripe_decrease_attachments ADD UNIQUE(quote_id,schedule_id);
CREATE TABLE alerts_stripe_decrease_schedules (
 quote_id uuid PRIMARY KEY,
 schedule_id text NOT NULL UNIQUE,
 attempts bigint NOT NULL DEFAULT 1 CHECK(attempts>0),
 lease_until timestamptz NOT NULL,
 snapshot jsonb CHECK(snapshot IS NULL OR jsonb_typeof(snapshot)='object'),
 verified_at timestamptz,
 CHECK((snapshot IS NULL)=(verified_at IS NULL)),
 FOREIGN KEY(quote_id,schedule_id) REFERENCES alerts_stripe_decrease_attachments(quote_id,schedule_id)
);
CREATE FUNCTION protect_alerts_decrease_schedule() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.quote_id IS DISTINCT FROM OLD.quote_id OR NEW.schedule_id IS DISTINCT FROM OLD.schedule_id
 OR NEW.attempts<OLD.attempts
 OR (OLD.snapshot IS NOT NULL AND (NEW.snapshot IS DISTINCT FROM OLD.snapshot OR NEW.verified_at IS DISTINCT FROM OLD.verified_at)) THEN
 RAISE EXCEPTION 'Decrease schedule identity and verified evidence are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_decrease_schedule_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_schedules
FOR EACH ROW EXECUTE FUNCTION protect_alerts_decrease_schedule();

-- alerts stripe quantity history
CREATE TABLE alerts_stripe_decrease_transitions (
 quote_id uuid PRIMARY KEY REFERENCES alerts_stripe_decrease_schedules(quote_id),
 checkout_id uuid NOT NULL,
 binding jsonb NOT NULL CHECK(jsonb_typeof(binding)='object'),
 transition jsonb NOT NULL CHECK(jsonb_typeof(transition)='object'),
 effective_at timestamptz NOT NULL,
 observed_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(quote_id,checkout_id) REFERENCES alerts_stripe_decrease_quotes(id,checkout_id)
);
CREATE TRIGGER alerts_stripe_decrease_transition_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_transitions
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();

-- One ordering for both paid upgrades and verified renewal decreases. Account
-- locks serialize writers; old checkout bindings and invoice facts stay intact.
CREATE TABLE alerts_stripe_quantity_history (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 checkout_id uuid NOT NULL REFERENCES service_stripe_subscriptions(checkout_id),
 binding jsonb NOT NULL CHECK(jsonb_typeof(binding)='object'),
 increase_quote_id uuid UNIQUE REFERENCES alerts_stripe_increase_payments(quote_id),
 decrease_quote_id uuid UNIQUE REFERENCES alerts_stripe_decrease_transitions(quote_id),
 CHECK(num_nonnulls(increase_quote_id,decrease_quote_id)=1)
);
CREATE INDEX alerts_stripe_quantity_latest ON alerts_stripe_quantity_history(checkout_id,sequence DESC);
INSERT INTO alerts_stripe_quantity_history(checkout_id,binding,increase_quote_id)
 SELECT checkout_id,binding,quote_id FROM alerts_stripe_increase_payments ORDER BY sequence;
CREATE TRIGGER alerts_stripe_quantity_history_protected BEFORE UPDATE OR DELETE ON alerts_stripe_quantity_history
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();
CREATE FUNCTION append_alerts_stripe_quantity_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='alerts_stripe_increase_payments' THEN
 INSERT INTO alerts_stripe_quantity_history(checkout_id,binding,increase_quote_id) VALUES(NEW.checkout_id,NEW.binding,NEW.quote_id);
 ELSE
 INSERT INTO alerts_stripe_quantity_history(checkout_id,binding,decrease_quote_id) VALUES(NEW.checkout_id,NEW.binding,NEW.quote_id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_stripe_increase_quantity AFTER INSERT ON alerts_stripe_increase_payments
FOR EACH ROW EXECUTE FUNCTION append_alerts_stripe_quantity_history();
CREATE TRIGGER alerts_stripe_decrease_quantity AFTER INSERT ON alerts_stripe_decrease_transitions
FOR EACH ROW EXECUTE FUNCTION append_alerts_stripe_quantity_history();
CREATE OR REPLACE VIEW alerts_stripe_current_subscriptions AS
SELECT s.checkout_id,s.subscription_id,s.created_at,
 COALESCE((SELECT h.binding FROM alerts_stripe_quantity_history h WHERE h.checkout_id=s.checkout_id ORDER BY h.sequence DESC LIMIT 1),s.binding) AS binding
FROM service_stripe_subscriptions s;

-- alerts stripe decrease withdrawals
CREATE TABLE alerts_stripe_decrease_withdrawals (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 quote_id uuid NOT NULL UNIQUE REFERENCES alerts_stripe_decrease_schedules(quote_id),
 created_at timestamptz NOT NULL DEFAULT now(),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','released','review')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 lease_until timestamptz NOT NULL DEFAULT now(),
 snapshot jsonb CHECK(snapshot IS NULL OR jsonb_typeof(snapshot)='object'),
 CHECK((state='released')=(snapshot IS NOT NULL))
);
CREATE FUNCTION protect_alerts_decrease_withdrawal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.quote_id IS DISTINCT FROM OLD.quote_id
 OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.attempts<OLD.attempts
 OR (OLD.state='released' AND (NEW.state IS DISTINCT FROM OLD.state OR NEW.snapshot IS DISTINCT FROM OLD.snapshot)) THEN
 RAISE EXCEPTION 'Decrease withdrawal identity and release evidence are immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_decrease_withdrawal_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_withdrawals
FOR EACH ROW EXECUTE FUNCTION protect_alerts_decrease_withdrawal();

-- service receipt jobs
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

-- service receipt recovery
ALTER TABLE service_receipt_jobs ADD COLUMN retry_after_attempt bigint NOT NULL DEFAULT 0
 CHECK(retry_after_attempt>=0 AND retry_after_attempt<=attempts);

CREATE TABLE service_receipt_recoveries (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 order_id uuid NOT NULL REFERENCES service_receipt_jobs(order_id) ON DELETE CASCADE,
 after_attempt bigint NOT NULL,
 actor text NOT NULL CHECK(length(actor) BETWEEN 1 AND 200),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 1000),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(order_id,after_attempt)
);

-- Insider shared integration: insider_subscriber_projection.sql

-- Informational only. Insider remains authoritative for access, grants,
-- purchases, stream consent, cadence, pauses, and notifications.
CREATE TABLE insider_subscriber_projections (
  person_id uuid PRIMARY KEY REFERENCES people(id) ON DELETE CASCADE,
  subscriber_tier text NOT NULL CHECK (subscriber_tier IN ('none','free','paid')),
  newsletter_status text NOT NULL CHECK (newsletter_status IN ('subscribed','not_subscribed')),
  version bigint NOT NULL CHECK (version > 0),
  as_of timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE insider_subscriber_projections IS
  'Minimal private dashboard projection from Insider; never authoritative for access or email delivery.';

CREATE TRIGGER insider_subscriber_projections_set_updated_at
BEFORE UPDATE ON insider_subscriber_projections
FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Verified external product fulfillment outbox; saved with payment evidence.
CREATE TABLE service_fulfillment_jobs (
 attempt_id uuid PRIMARY KEY REFERENCES service_payment_attempts(id) ON DELETE RESTRICT,
 payment jsonb NOT NULL CHECK(jsonb_typeof(payment)='object'),
 payload jsonb CHECK(jsonb_typeof(payload)='object'),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','review')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX service_fulfillment_due ON service_fulfillment_jobs(next_attempt_at,lease_until) WHERE state='pending';

-- Transitional Alerts adapters over shared records, not separate financial ledgers.
-- Filtering keeps Alerts workers and account checks away from Insider subscriptions.
CREATE VIEW alerts_stripe_checkouts AS SELECT * FROM service_stripe_checkouts
 WHERE product_family='alerts' WITH CASCADED CHECK OPTION;
CREATE VIEW alerts_stripe_subscriptions AS SELECT * FROM service_stripe_subscriptions
 WHERE checkout_id IN (SELECT id FROM service_stripe_checkouts WHERE product_family='alerts')
 WITH CASCADED CHECK OPTION;
CREATE VIEW alerts_stripe_invoices AS SELECT * FROM service_stripe_invoices
 WHERE checkout_id IN (SELECT id FROM service_stripe_checkouts WHERE product_family='alerts')
 WITH CASCADED CHECK OPTION;

-- Product-filtered compatibility projection for existing Alerts lifecycle readers.
CREATE VIEW alerts_stripe_subscription_state AS SELECT * FROM service_stripe_subscription_state
 WHERE checkout_id IN (SELECT id FROM service_stripe_checkouts WHERE product_family='alerts')
 WITH CASCADED CHECK OPTION;

CREATE VIEW alerts_stripe_cancellations AS SELECT * FROM service_stripe_cancellations
 WHERE checkout_id IN (SELECT id FROM service_stripe_checkouts WHERE product_family='alerts')
 WITH CASCADED CHECK OPTION;

-- Product lifecycle events are separate from financial settlement attempts.
CREATE TABLE service_lifecycle_jobs (
 checkout_id uuid NOT NULL REFERENCES service_stripe_subscriptions(checkout_id),
 revision bigint NOT NULL CHECK(revision>0),
 event jsonb NOT NULL CHECK(jsonb_typeof(event)='object'),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','review')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(checkout_id,revision)
);
CREATE INDEX service_lifecycle_due ON service_lifecycle_jobs(next_attempt_at,checkout_id,revision) WHERE state='pending';
CREATE FUNCTION protect_service_lifecycle_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.checkout_id IS DISTINCT FROM OLD.checkout_id
 OR NEW.revision IS DISTINCT FROM OLD.revision OR NEW.event IS DISTINCT FROM OLD.event
 OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.attempts<OLD.attempts
 OR (OLD.state='accepted' AND NEW.state<>'accepted') THEN
  RAISE EXCEPTION 'Service lifecycle evidence cannot be rewritten' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER service_lifecycle_protected BEFORE UPDATE OR DELETE ON service_lifecycle_jobs
 FOR EACH ROW EXECUTE FUNCTION protect_service_lifecycle_event();

-- Refund delivery is queued atomically with financial acceptance, independently
-- of the product's eventual access-adjustment policy.
CREATE TABLE service_refund_jobs (
 provider text NOT NULL,
 refund_reference text NOT NULL,
 attempt_id uuid NOT NULL REFERENCES service_payment_attempts(id),
 revision bigint NOT NULL CHECK(revision>0),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','review')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 payload jsonb CHECK(payload IS NULL OR jsonb_typeof(payload)='object'),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(provider,refund_reference),
 UNIQUE(attempt_id,revision),
 FOREIGN KEY(provider,refund_reference) REFERENCES service_payment_refunds(provider,refund_reference)
);
CREATE INDEX service_refund_jobs_due ON service_refund_jobs(next_attempt_at,attempt_id,revision) WHERE state='pending';
CREATE FUNCTION protect_service_refund_job() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.provider IS DISTINCT FROM OLD.provider
 OR NEW.refund_reference IS DISTINCT FROM OLD.refund_reference
 OR NEW.attempt_id IS DISTINCT FROM OLD.attempt_id OR NEW.revision IS DISTINCT FROM OLD.revision
 OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.attempts<OLD.attempts
 OR (OLD.payload IS NOT NULL AND NEW.payload IS DISTINCT FROM OLD.payload)
 OR (OLD.state='accepted' AND NEW.state<>'accepted') THEN
  RAISE EXCEPTION 'Refund delivery evidence cannot be rewritten' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER service_refund_job_protected BEFORE UPDATE OR DELETE ON service_refund_jobs
 FOR EACH ROW EXECUTE FUNCTION protect_service_refund_job();

CREATE TABLE service_refund_access_decisions (
 provider text NOT NULL,
 refund_reference text NOT NULL,
 action text NOT NULL CHECK(action IN ('retain','revoke')),
 actor_id uuid NOT NULL,
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 1000),
 decided_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(provider,refund_reference),
 FOREIGN KEY(provider,refund_reference) REFERENCES service_payment_refunds(provider,refund_reference)
);
CREATE TRIGGER service_refund_access_decision_immutable BEFORE UPDATE OR DELETE ON service_refund_access_decisions
 FOR EACH ROW EXECUTE FUNCTION protect_service_payment_event();

-- No person FK: delivery must survive removal of the source account. Identity is
-- the removed UUID, never its replacement historical attribution identity.
CREATE TABLE account_deletion_deliveries (
 person_id uuid NOT NULL,
 service text NOT NULL CHECK(service IN ('alerts','insider')),
 PRIMARY KEY(person_id,service),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 created_at timestamptz NOT NULL DEFAULT now(),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX account_deletion_deliveries_due ON account_deletion_deliveries(next_attempt_at) WHERE state='pending';
CREATE FUNCTION protect_account_deletion_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.service IS DISTINCT FROM OLD.service OR NEW.created_at IS DISTINCT FROM OLD.created_at
 OR (OLD.state='accepted' AND NEW.state<>'accepted') THEN
  RAISE EXCEPTION 'account deletion delivery identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER account_deletion_delivery_protected BEFORE UPDATE OR DELETE ON account_deletion_deliveries
 FOR EACH ROW EXECUTE FUNCTION protect_account_deletion_delivery();

-- Capture every recurring checkout before its nullable person link is cleared.
-- An unbound checkout can still be in flight at Stripe and must be reconciled.
CREATE TABLE account_deletion_cancellations (
 checkout_id uuid PRIMARY KEY REFERENCES service_stripe_checkouts(id),
 person_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 requested_at timestamptz NOT NULL DEFAULT now(),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','confirmed','review')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX account_deletion_cancellations_due ON account_deletion_cancellations(next_attempt_at) WHERE state='pending';
CREATE FUNCTION protect_account_deletion_cancellation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.checkout_id IS DISTINCT FROM OLD.checkout_id OR NEW.person_id IS DISTINCT FROM OLD.person_id
 OR NEW.actor_id IS DISTINCT FROM OLD.actor_id OR NEW.requested_at IS DISTINCT FROM OLD.requested_at
 OR (OLD.state='confirmed' AND NEW.state<>'confirmed') THEN
  RAISE EXCEPTION 'account deletion cancellation identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER account_deletion_cancellation_protected BEFORE UPDATE OR DELETE ON account_deletion_cancellations
 FOR EACH ROW EXECUTE FUNCTION protect_account_deletion_cancellation();

CREATE TABLE account_deletion_cancellation_recoveries (
 checkout_id uuid NOT NULL REFERENCES account_deletion_cancellations(checkout_id),
 after_attempt bigint NOT NULL CHECK(after_attempt>0),
 actor_id uuid NOT NULL,
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 1000),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(checkout_id,after_attempt)
);
CREATE TRIGGER account_deletion_cancellation_recovery_immutable BEFORE UPDATE OR DELETE ON account_deletion_cancellation_recoveries
 FOR EACH ROW EXECUTE FUNCTION protect_service_payment_event();

-- Reviewed service policies are product-specific and independent of customer data.
CREATE TABLE service_legal_revisions (
 product_code text NOT NULL CHECK(product_code IN ('alerts.repository-slots','insider.membership','insider.article','insider.membership.monthly','insider.membership.annual')),
 version text NOT NULL CHECK(length(version) BETWEEN 1 AND 200),
 terms_text text NOT NULL CHECK(length(terms_text)>0),
 privacy_text text NOT NULL CHECK(length(privacy_text)>0),
 content_sha256 text NOT NULL CHECK(length(content_sha256)=64),
 effective_at timestamptz NOT NULL,
 PRIMARY KEY(product_code,version)
);
CREATE TRIGGER service_legal_revision_immutable BEFORE UPDATE OR DELETE ON service_legal_revisions
 FOR EACH ROW EXECUTE FUNCTION protect_service_payment_event();
CREATE TABLE service_legal_current (
 product_code text PRIMARY KEY,
 version text NOT NULL,
 FOREIGN KEY(product_code,version) REFERENCES service_legal_revisions(product_code,version)
);

-- Authenticated Bitcoin price evidence is frozen with the payment event. Legacy
-- or non-OpenNode events may have no evidence and cannot establish a BTC refund.
ALTER TABLE service_payment_events ADD COLUMN opennode_bitcoin jsonb
 CHECK(opennode_bitcoin IS NULL OR
   (provider='opennode' AND jsonb_typeof(opennode_bitcoin)='object'
    AND opennode_bitcoin ? 'price_satoshis'
    AND jsonb_typeof(opennode_bitcoin->'price_satoshis')='number'
    AND (opennode_bitcoin->>'price_satoshis')::numeric BETWEEN 1 AND 2100000000000000
    AND trunc((opennode_bitcoin->>'price_satoshis')::numeric)=(opennode_bitcoin->>'price_satoshis')::numeric));

ALTER TABLE service_payment_refunds ADD COLUMN amount_satoshis bigint
 CHECK(amount_satoshis IS NULL OR (provider='opennode' AND amount_satoshis BETWEEN 1 AND 2100000000000000));

-- A reviewed quote is an immutable audit record, not an executed refund or
-- permission to send a stale payout. Execution must revalidate its full basis.
CREATE TABLE service_bitcoin_closure_reviews (
 id uuid PRIMARY KEY,
 attempt_id uuid NOT NULL REFERENCES service_payment_attempts(id),
 actor_id uuid NOT NULL REFERENCES people(id),
 closure_at timestamptz NOT NULL,
 amount_satoshis bigint NOT NULL CHECK(amount_satoshis BETWEEN 1 AND 2100000000000000),
 rounding text NOT NULL DEFAULT 'floor_satoshi' CHECK(rounding='floor_satoshi'),
 basis jsonb NOT NULL CHECK(jsonb_typeof(basis)='object'),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 1000),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX service_bitcoin_closure_reviews_attempt ON service_bitcoin_closure_reviews(attempt_id,created_at);
CREATE TRIGGER service_bitcoin_closure_reviews_immutable BEFORE UPDATE OR DELETE ON service_bitcoin_closure_reviews
FOR EACH ROW EXECUTE FUNCTION protect_service_payment_event();

-- One approved recipient per immutable review. A new destination requires a new
-- review; existing approval evidence is never rewritten by a retry or account merge.
CREATE TABLE service_bitcoin_closure_destinations (
 review_id uuid PRIMARY KEY REFERENCES service_bitcoin_closure_reviews(id),
 actor_id uuid NOT NULL REFERENCES people(id),
 chain text NOT NULL CHECK(chain IN ('main','regtest','signet','test','testnet4')),
 destination text NOT NULL CHECK(length(destination) BETWEEN 1 AND 128),
 script_pub_key text NOT NULL CHECK(length(script_pub_key) BETWEEN 2 AND 20000 AND script_pub_key ~ '^([0-9a-f]{2})+$'),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 1000),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER service_bitcoin_closure_destinations_immutable BEFORE UPDATE OR DELETE ON service_bitcoin_closure_destinations
FOR EACH ROW EXECUTE FUNCTION protect_service_payment_event();
