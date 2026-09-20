BEGIN;
CREATE TABLE sponsor_report_drafts (
  scope text PRIMARY KEY,
  content jsonb NOT NULL,
  revision bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT now()
);
COMMIT;
