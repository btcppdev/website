-- Discount deletion is destructive in the admin UI. Archived rows were never
-- visible there and prevented code names from being reused through the unique
-- constraint, so remove legacy tombstones and the obsolete archive mechanism.
DELETE FROM discounts WHERE archived_at IS NOT NULL;

ALTER TABLE discounts DROP COLUMN archived_at;
