-- Read-only inventory for retiring the previous publishing service.
-- Cached rows and the historical ledger are NOT a complete or current remote queue.
BEGIN TRANSACTION READ ONLY;
SELECT ref, posted_to, status, scheduled_at, posted_at, url, text
FROM social_posts
WHERE posted_to IN ('buffer','buffer-twitter')
ORDER BY scheduled_at NULLS LAST, ref;

SELECT e.remote_id, c.platform, c.name AS account, e.title, e.state,
       e.starts_at, e.url, e.updated_at AS snapshot_updated_at
FROM publishing_external e JOIN publishing_channels c ON c.id=e.channel_id
WHERE e.provider='buffer'
ORDER BY e.starts_at NULLS LAST, e.remote_id;
COMMIT;
