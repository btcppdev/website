CREATE TABLE alerts_stripe_subscriptions (
 checkout_id uuid PRIMARY KEY REFERENCES alerts_stripe_checkouts(id),
 subscription_id text NOT NULL UNIQUE,
 binding jsonb NOT NULL CHECK(jsonb_typeof(binding)='object'),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE alerts_stripe_invoices (
 invoice_id text PRIMARY KEY,
 checkout_id uuid NOT NULL REFERENCES alerts_stripe_subscriptions(checkout_id),
 order_id uuid NOT NULL UNIQUE REFERENCES shop_orders(id),
 paid_period jsonb NOT NULL CHECK(jsonb_typeof(paid_period)='object'),
 starts_at timestamptz NOT NULL,
 ends_at timestamptz NOT NULL CHECK(ends_at>starts_at),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alerts_stripe_invoice_periods ON alerts_stripe_invoices(checkout_id,starts_at,ends_at);
CREATE FUNCTION protect_alerts_stripe_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'Stripe subscription and invoice evidence is immutable' USING ERRCODE='23514';
END;
$$;
CREATE TRIGGER alerts_stripe_subscription_protected BEFORE UPDATE OR DELETE ON alerts_stripe_subscriptions
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();
CREATE TRIGGER alerts_stripe_invoice_protected BEFORE UPDATE OR DELETE ON alerts_stripe_invoices
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();
