BEGIN;

CREATE TABLE competitions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
  slug text NOT NULL UNIQUE,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  description_format text NOT NULL DEFAULT 'markdown',
  visibility text NOT NULL DEFAULT 'hidden',
  lifecycle_override text NOT NULL DEFAULT '',
  judging_mode text NOT NULL DEFAULT 'automatic',
  public_gallery_enabled boolean NOT NULL DEFAULT false,
  allow_late_submissions boolean NOT NULL DEFAULT false,
  public_tables_enabled boolean NOT NULL DEFAULT false,
  max_team_size integer,
  submissions_open_at timestamptz,
  submissions_close_at timestamptz,
  public_gallery_at timestamptz,
  hacking_starts_at timestamptz,
  hacking_ends_at timestamptz,
  judges_meeting_at timestamptz,
  expo_starts_at timestamptz,
  expo_ends_at timestamptz,
  expo_judging_starts_at timestamptz,
  expo_judging_ends_at timestamptz,
  finals_starts_at timestamptz,
  finals_ends_at timestamptz,
  finals_judging_starts_at timestamptz,
  finals_judging_ends_at timestamptz,
  awards_ceremony_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (slug <> ''),
  CHECK (title <> ''),
  CHECK (description_format IN ('plain', 'markdown', 'html')),
  CHECK (visibility IN ('hidden', 'public')),
  CHECK (lifecycle_override IN ('', 'upcoming', 'open', 'submissions_closed', 'closed')),
  CHECK (judging_mode IN ('manual', 'automatic')),
  CHECK (max_team_size IS NULL OR max_team_size > 0),
  CHECK (submissions_close_at IS NULL OR submissions_open_at IS NULL OR submissions_close_at >= submissions_open_at),
  CHECK (hacking_ends_at IS NULL OR hacking_starts_at IS NULL OR hacking_ends_at >= hacking_starts_at),
  CHECK (expo_ends_at IS NULL OR expo_starts_at IS NULL OR expo_ends_at >= expo_starts_at),
  CHECK (expo_judging_ends_at IS NULL OR expo_judging_starts_at IS NULL OR expo_judging_ends_at >= expo_judging_starts_at),
  CHECK (finals_ends_at IS NULL OR finals_starts_at IS NULL OR finals_ends_at >= finals_starts_at),
  CHECK (finals_judging_ends_at IS NULL OR finals_judging_starts_at IS NULL OR finals_judging_ends_at >= finals_judging_starts_at)
);

CREATE UNIQUE INDEX competitions_conference_idx ON competitions (conference_id);
CREATE INDEX competitions_visibility_idx ON competitions (visibility);

CREATE TRIGGER competitions_set_updated_at
BEFORE UPDATE ON competitions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE competition_hackers (
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'looking_for_team',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (competition_id, person_id),
  CHECK (status IN ('looking_for_team', 'on_team', 'inactive'))
);

CREATE INDEX competition_hackers_person_idx ON competition_hackers (person_id);

CREATE TRIGGER competition_hackers_set_updated_at
BEFORE UPDATE ON competition_hackers
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE competition_schedule_segments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  proposal_id uuid REFERENCES proposals(id) ON DELETE SET NULL,
  conf_talk_id uuid REFERENCES conf_talks(id) ON DELETE SET NULL,
  segment_type text NOT NULL DEFAULT 'custom',
  title text NOT NULL,
  default_duration_minutes integer NOT NULL DEFAULT 30,
  ordering integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (segment_type <> ''),
  CHECK (title <> ''),
  CHECK (default_duration_minutes > 0),
  CHECK (ordering >= 0)
);

CREATE INDEX competition_schedule_segments_competition_idx
ON competition_schedule_segments (competition_id, ordering);

CREATE INDEX competition_schedule_segments_proposal_idx
ON competition_schedule_segments (proposal_id);

CREATE TRIGGER competition_schedule_segments_set_updated_at
BEFORE UPDATE ON competition_schedule_segments
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE projects (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  created_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  slug text NOT NULL,
  title text NOT NULL,
  short_description text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  description_format text NOT NULL DEFAULT 'markdown',
  image_url text NOT NULL DEFAULT '',
  image_urls text[] NOT NULL DEFAULT '{}',
  github_url text NOT NULL DEFAULT '',
  demo_url text NOT NULL DEFAULT '',
  video_url text NOT NULL DEFAULT '',
  slides_url text NOT NULL DEFAULT '',
  docs_url text NOT NULL DEFAULT '',
  project_number integer,
  status text NOT NULL DEFAULT 'created',
  tags text[] NOT NULL DEFAULT '{}',
  submitted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (competition_id, slug),
  UNIQUE (competition_id, project_number),
  CHECK (slug <> ''),
  CHECK (title <> ''),
  CHECK (description_format IN ('plain', 'markdown', 'html')),
  CHECK (project_number IS NULL OR project_number > 0),
  CHECK (status IN ('created', 'submitted', 'hidden', 'advanced'))
);

CREATE INDEX projects_competition_status_idx ON projects (competition_id, status);
CREATE INDEX projects_tags_idx ON projects USING gin (tags);

CREATE TRIGGER projects_set_updated_at
BEFORE UPDATE ON projects
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE project_members (
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  role text NOT NULL DEFAULT 'member',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, person_id),
  CHECK (role IN ('owner', 'member'))
);

CREATE UNIQUE INDEX project_one_owner_idx
ON project_members (project_id)
WHERE role = 'owner';

CREATE INDEX project_members_person_idx ON project_members (person_id);

CREATE TABLE project_invites (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  email citext,
  accepted_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  accepted_at timestamptz,
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (token_hash <> '')
);

CREATE INDEX project_invites_project_idx ON project_invites (project_id);
CREATE INDEX project_invites_email_idx ON project_invites (email);

CREATE TABLE competition_judges (
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  judge_type text NOT NULL DEFAULT 'expo',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (competition_id, person_id, judge_type),
  CHECK (judge_type IN ('expo', 'finals', 'coordinator'))
);

CREATE INDEX competition_judges_person_idx ON competition_judges (person_id);

CREATE TABLE competition_judge_invites (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  accepted_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  accepted_at timestamptz,
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (token_hash <> '')
);

CREATE INDEX competition_judge_invites_competition_idx ON competition_judge_invites (competition_id);

CREATE TABLE judge_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  schedule_segment_id uuid NOT NULL REFERENCES competition_schedule_segments(id) ON DELETE CASCADE,
  name text NOT NULL,
  playbook_type text NOT NULL,
  state text NOT NULL DEFAULT 'pending',
  ordering integer NOT NULL DEFAULT 0,
  starts_at timestamptz,
  ends_at timestamptz,
  starting_project_number integer,
  rank_limit integer NOT NULL DEFAULT 4,
  cal_notif text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (name <> ''),
  CHECK (playbook_type IN ('expo', 'finals')),
  CHECK (state IN ('pending', 'open', 'closed')),
  CHECK (ordering >= 0),
  CHECK (starting_project_number IS NULL OR starting_project_number > 0),
  CHECK (rank_limit > 0),
  CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at >= starts_at)
);

CREATE INDEX judge_events_competition_idx ON judge_events (competition_id, ordering);

CREATE INDEX judge_events_competition_state_idx
ON judge_events (competition_id, state, ordering);

CREATE UNIQUE INDEX judge_events_schedule_segment_idx
ON judge_events (schedule_segment_id)
WHERE schedule_segment_id IS NOT NULL;

CREATE TRIGGER judge_events_set_updated_at
BEFORE UPDATE ON judge_events
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE scorecards (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  judge_event_id uuid NOT NULL REFERENCES judge_events(id) ON DELETE CASCADE,
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  judge_person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  rank integer,
  comments text NOT NULL DEFAULT '',
  submitted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (judge_event_id, project_id, judge_person_id),
  CHECK (rank IS NULL OR rank > 0)
);

CREATE INDEX scorecards_judge_event_idx ON scorecards (judge_event_id);
CREATE INDEX scorecards_project_idx ON scorecards (project_id);
CREATE INDEX scorecards_judge_person_idx ON scorecards (judge_person_id);

CREATE TRIGGER scorecards_set_updated_at
BEFORE UPDATE ON scorecards
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE awards (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  sponsored_by_org_id uuid REFERENCES organizations(id) ON DELETE SET NULL,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  photo_url text NOT NULL DEFAULT '',
  max_awardees integer,
  opt_in_required boolean NOT NULL DEFAULT false,
  status text NOT NULL DEFAULT 'draft',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  archived_at timestamptz,
  CHECK (title <> ''),
  CHECK (max_awardees IS NULL OR max_awardees > 0),
  CHECK (status IN ('draft', 'available', 'unawarded', 'awarded'))
);

CREATE INDEX awards_competition_idx ON awards (competition_id);
CREATE INDEX awards_competition_active_idx ON awards (competition_id, archived_at);
CREATE INDEX awards_sponsor_idx ON awards (sponsored_by_org_id);

CREATE TRIGGER awards_set_updated_at
BEFORE UPDATE ON awards
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE prizes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  award_id uuid NOT NULL REFERENCES awards(id) ON DELETE CASCADE,
  prize_type text NOT NULL,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  value_text text NOT NULL DEFAULT '',
  pool_percentage numeric(5,2),
  pool_url text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'available',
  comments text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (title <> ''),
  CHECK (prize_type IN ('sats', 'in_kind', 'tickets', 'pooled', 'trophy')),
  CHECK (pool_percentage IS NULL OR (pool_percentage >= 0 AND pool_percentage <= 100)),
  CHECK (status IN ('available', 'needs_funds', 'awarded', 'paid'))
);

CREATE INDEX prizes_award_idx ON prizes (award_id);

CREATE TRIGGER prizes_set_updated_at
BEFORE UPDATE ON prizes
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE project_awards (
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  award_id uuid NOT NULL REFERENCES awards(id) ON DELETE CASCADE,
  awarded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, award_id)
);

CREATE TABLE project_award_opt_ins (
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  award_id uuid NOT NULL REFERENCES awards(id) ON DELETE CASCADE,
  opted_in_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, award_id)
);

COMMIT;
