BEGIN;

ALTER TABLE conference_email_campaigns
  ADD COLUMN template_missive_id uuid REFERENCES missives(id) ON DELETE SET NULL;

CREATE INDEX conference_email_campaigns_template_idx
ON conference_email_campaigns (template_missive_id)
WHERE template_missive_id IS NOT NULL;

COMMIT;
