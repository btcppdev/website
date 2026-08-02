-- Social post refs are idempotency keys. Keep the most useful recent row for
-- historical duplicates before enforcing that invariant.
WITH ranked AS (
  SELECT id,
    row_number() OVER (
      PARTITION BY ref
      ORDER BY
        (url <> '') DESC,
        (posted_at IS NOT NULL) DESC,
        updated_at DESC,
        created_at DESC,
        id DESC
    ) AS position
  FROM social_posts
)
DELETE FROM social_posts posts
USING ranked
WHERE posts.id = ranked.id
  AND ranked.position > 1;

DROP INDEX IF EXISTS social_posts_ref_idx;
CREATE UNIQUE INDEX social_posts_ref_unique_idx ON social_posts (ref);

ALTER TABLE social_posts
  ADD COLUMN publication_claim_token uuid,
  ADD COLUMN publication_claim_expires_at timestamptz;

ALTER TABLE social_posts
  ADD CONSTRAINT social_posts_publication_claim_pair_check
  CHECK (
    (publication_claim_token IS NULL) =
    (publication_claim_expires_at IS NULL)
  );
