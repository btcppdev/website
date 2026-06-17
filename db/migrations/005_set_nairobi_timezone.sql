BEGIN;

UPDATE conferences
SET timezone = 'Africa/Nairobi'
WHERE tag = 'nairobi'
  AND (timezone = '' OR timezone = 'UTC');

COMMIT;
