ALTER TABLE merch_products
ADD COLUMN published_at timestamptz;

UPDATE merch_products
SET published_at = created_at
WHERE status = 'published'
  AND published_at IS NULL;

CREATE INDEX merch_products_published_at_idx
ON merch_products (published_at)
WHERE status = 'published';
