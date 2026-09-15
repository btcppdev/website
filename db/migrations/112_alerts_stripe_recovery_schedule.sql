ALTER TABLE alerts_stripe_checkouts ADD COLUMN next_reconcile_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX alerts_stripe_checkout_recovery_due ON alerts_stripe_checkouts(next_reconcile_at,id)
 WHERE closed_at IS NULL AND person_id IS NOT NULL AND stripe_session_id IS NOT NULL;
