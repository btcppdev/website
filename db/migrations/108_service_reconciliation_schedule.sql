ALTER TABLE service_payment_attempts ADD COLUMN next_reconcile_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX service_payment_reconcile_due ON service_payment_attempts(next_reconcile_at,id)
WHERE provider='opennode' AND provider_reference IS NOT NULL;
