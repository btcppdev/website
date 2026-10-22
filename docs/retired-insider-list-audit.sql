-- Read-only production audit. No subscriber addresses are returned.
BEGIN READ ONLY;
SELECT count(*) AS legacy_insider_memberships
FROM subscriber_subscriptions WHERE name = 'insider';

-- Review ALL matches: sent_at alone cannot rule out queued or recurring mail.
-- Include exclusions: simply removing !insider would widen an audience.
SELECT id, public_uid, title, newsletters, send_at_expr, sent_at, expiry
FROM missives
WHERE EXISTS (
 SELECT 1 FROM unnest(newsletters) AS target(name)
 WHERE btrim(regexp_replace(btrim(name), '^!', '')) = 'insider'
)
ORDER BY sent_at NULLS FIRST, public_uid;
ROLLBACK;

-- Also inspect the mailer queue for these campaign identifiers before retiring
-- live campaigns. The application cannot recall jobs already accepted there.
-- Preserve subscriber rows, historical membership and unsubscribe records.
