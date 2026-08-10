BEGIN;

-- Checkout has always treated this as the moment a tier stops being current.
-- Give the column a name that matches that behavior.
ALTER TABLE conference_tickets
RENAME COLUMN expires_start TO sales_end_at;

CREATE INDEX conference_tickets_sales_end_at_idx
ON conference_tickets (sales_end_at)
WHERE sales_end_at IS NOT NULL;

CREATE INDEX recordings_publish_at_idx
ON recordings (publish_at)
WHERE publish_at IS NOT NULL;

CREATE INDEX speaker_confs_accepted_at_idx
ON speaker_confs (accepted_at)
WHERE accepted_at IS NOT NULL;

CREATE INDEX competitions_results_finalized_at_idx
ON competitions (results_finalized_at)
WHERE results_finalized_at IS NOT NULL;

COMMIT;
