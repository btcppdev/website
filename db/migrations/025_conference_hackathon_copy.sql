ALTER TABLE conferences
  ADD COLUMN IF NOT EXISTS hackathon_section_label text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS hackathon_headline text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS hackathon_judges_note text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS hackathon_proof_label text NOT NULL DEFAULT '';
