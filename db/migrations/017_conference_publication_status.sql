ALTER TABLE conferences
ADD COLUMN IF NOT EXISTS publication_status text NOT NULL DEFAULT 'draft';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'conferences_publication_status_check'
  ) THEN
    ALTER TABLE conferences
    ADD CONSTRAINT conferences_publication_status_check
    CHECK (publication_status IN ('draft', 'published'));
  END IF;
END $$;

UPDATE conferences
SET publication_status = CASE
  WHEN active OR (end_date IS NOT NULL AND end_date < now()) THEN 'published'
  ELSE 'draft'
END
WHERE publication_status = 'draft';

UPDATE conferences
SET active = publication_status = 'published'
  AND (end_date IS NULL OR end_date >= now());
