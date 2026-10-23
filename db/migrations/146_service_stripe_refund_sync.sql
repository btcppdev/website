CREATE TABLE service_stripe_refund_checks (
 attempt_id uuid PRIMARY KEY REFERENCES service_payment_attempts(id),
 next_check_at timestamptz NOT NULL DEFAULT now(),
 last_success_at timestamptz,
 failures integer NOT NULL DEFAULT 0,
 last_error text NOT NULL DEFAULT ''
);
CREATE INDEX service_stripe_refund_checks_due ON service_stripe_refund_checks(next_check_at);
