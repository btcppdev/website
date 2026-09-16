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
