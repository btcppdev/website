ALTER TABLE competitions
  ADD COLUMN judging_mode text NOT NULL DEFAULT 'automatic',
  ADD CONSTRAINT competitions_judging_mode_check CHECK (judging_mode IN ('manual', 'automatic'));
