ALTER TABLE speaker_confs
  ADD COLUMN IF NOT EXISTS featured_rank integer;

ALTER TABLE speaker_confs
  DROP CONSTRAINT IF EXISTS speaker_confs_featured_rank_check;

ALTER TABLE speaker_confs
  ADD CONSTRAINT speaker_confs_featured_rank_check
  CHECK (featured_rank IS NULL OR featured_rank BETWEEN 1 AND 6);
