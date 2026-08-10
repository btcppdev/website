BEGIN;

CREATE TABLE weekly_newsletter_featured_talks (
  missive_id uuid PRIMARY KEY REFERENCES missives(id) ON DELETE CASCADE,
  conf_talk_id uuid NOT NULL REFERENCES conf_talks(id) ON DELETE CASCADE,
  selected_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX weekly_newsletter_featured_talks_talk_idx
ON weekly_newsletter_featured_talks (conf_talk_id);

COMMIT;
