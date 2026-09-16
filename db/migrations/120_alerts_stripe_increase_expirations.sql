CREATE TABLE alerts_stripe_increase_expirations (
 quote_id uuid PRIMARY KEY REFERENCES alerts_stripe_increases(quote_id),
 invoice_id text NOT NULL UNIQUE,
 subscription_state jsonb NOT NULL CHECK(jsonb_typeof(subscription_state)='object'),
 observed_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER alerts_stripe_increase_expiration_protected BEFORE UPDATE OR DELETE ON alerts_stripe_increase_expirations
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();
