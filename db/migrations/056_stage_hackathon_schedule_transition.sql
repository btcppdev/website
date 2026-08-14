-- Keep the legacy timestamps while competitions transition to conference
-- schedule segments. The application prefers scheduled events and only falls
-- back to these values when an equivalent scheduled event does not exist.
COMMENT ON COLUMN competitions.submissions_open_at IS
  'Legacy fallback until this competition has a scheduled opening event';
COMMENT ON COLUMN competitions.submissions_close_at IS
  'Legacy fallback until this competition has a scheduled judging event';
COMMENT ON COLUMN competitions.public_gallery_at IS
  'Legacy display value retained until schedule migration is complete';
