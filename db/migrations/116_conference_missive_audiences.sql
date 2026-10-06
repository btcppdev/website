-- Repair event campaign draft metadata without changing recipients or sent mail.
UPDATE missives m
SET newsletters = ARRAY[conf.tag || '-' || CASE c.audience
    WHEN 'speakers' THEN 'speaker'
    WHEN 'attendees' THEN 'genpop'
    WHEN 'volunteers' THEN 'volunteer'
END]
FROM conference_email_occurrences o
JOIN conference_email_campaigns c ON c.id = o.campaign_id
JOIN conferences conf ON conf.id = c.conference_id
WHERE m.id = o.missive_id
  AND m.sent_at IS NULL
  AND o.sent_at IS NULL
  AND o.status IN ('planned', 'building', 'draft', 'paused', 'failed', 'skipped')
  AND c.audience IN ('speakers', 'attendees', 'volunteers');
