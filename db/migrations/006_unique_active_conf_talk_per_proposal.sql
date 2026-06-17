BEGIN;

-- A proposal should only have one active ConfTalk placement. Keep the
-- row most likely to be the organizer's latest intended placement and
-- soft-archive older duplicates.
WITH ranked AS (
  SELECT
    id,
    row_number() OVER (
      PARTITION BY proposal_id
      ORDER BY
        (cal_notif <> '') DESC,
        (scheduled_start IS NOT NULL) DESC,
        updated_at DESC,
        created_at DESC,
        id DESC
    ) AS rn
  FROM conf_talks
  WHERE proposal_id IS NOT NULL
    AND archived_at IS NULL
)
UPDATE conf_talks ct
SET archived_at = now()
FROM ranked
WHERE ct.id = ranked.id
  AND ranked.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS conf_talks_one_active_per_proposal_idx
ON conf_talks (proposal_id)
WHERE proposal_id IS NOT NULL
  AND archived_at IS NULL;

COMMIT;
