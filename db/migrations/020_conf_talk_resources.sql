ALTER TABLE conf_talks
  ADD COLUMN github_repo_url text NOT NULL DEFAULT '',
  ADD COLUMN slides_url text NOT NULL DEFAULT '';
