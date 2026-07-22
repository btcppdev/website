ALTER TABLE competition_judges
ADD COLUMN IF NOT EXISTS public_label_override text NOT NULL DEFAULT '';
