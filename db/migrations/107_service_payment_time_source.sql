-- OpenNode charge lookup establishes paid status, not a documented payment time.
-- Distinguish the first verified observation from provider-supplied timestamps.
ALTER TABLE service_payment_events ADD COLUMN time_source text NOT NULL DEFAULT 'provider'
  CHECK (time_source IN ('provider','first_verified_observation'));
