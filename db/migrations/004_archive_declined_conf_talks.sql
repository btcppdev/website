BEGIN;

-- Backfill stale scheduled placements for proposals that have since
-- reached a terminal declined/rejected state. ConfTalk rows are
-- soft-archived so historical data and assets remain available, while
-- schedule-derived views stop rendering them.
UPDATE conf_talks ct
SET archived_at = now()
FROM proposals p
WHERE ct.proposal_id = p.id
  AND ct.archived_at IS NULL
  AND p.status IN ('TheyDecline', 'WeDecline', 'Rejected');

COMMIT;
