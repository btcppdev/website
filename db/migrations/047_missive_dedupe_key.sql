BEGIN;

ALTER TABLE missives ADD COLUMN dedupe_key text;

CREATE UNIQUE INDEX missives_dedupe_key_idx
ON missives (dedupe_key)
WHERE dedupe_key IS NOT NULL;

COMMIT;
