-- An explicit checkout preference, never inferred from email or event location.
ALTER TABLE registrations ADD COLUMN locale text NOT NULL DEFAULT 'en'
  CHECK (locale IN ('en', 'ko'));
