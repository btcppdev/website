ALTER TABLE conferences
 ADD COLUMN speaker_applications_open timestamptz,
 ADD COLUMN volunteer_applications_open timestamptz,
 ADD COLUMN volunteer_applications_close timestamptz,
 ADD COLUMN student_ticket_body text NOT NULL DEFAULT '',
 ADD COLUMN leading_image_url text NOT NULL DEFAULT '',
 ADD COLUMN logo_black_url text NOT NULL DEFAULT '',
 ADD COLUMN logo_white_url text NOT NULL DEFAULT '';
ALTER TABLE conference_tickets ADD COLUMN student_price integer CHECK (student_price >= 0);
CREATE TABLE event_plans (
 conference_id uuid PRIMARY KEY REFERENCES conferences(id) ON DELETE CASCADE,
 revision bigint NOT NULL DEFAULT 0,
 data jsonb NOT NULL DEFAULT '{}',
 updated_by uuid REFERENCES people(id) ON DELETE SET NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE event_team_members (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
 email citext NOT NULL,
 name text NOT NULL DEFAULT '',
 responsibility text NOT NULL DEFAULT '',
 role text NOT NULL CHECK (role IN ('steering','staff','volcoord','admin')),
 person_id uuid REFERENCES people(id) ON DELETE SET NULL,
 invited_by uuid REFERENCES people(id) ON DELETE SET NULL,
 token_hash text NOT NULL DEFAULT '',
 expires_at timestamptz,
 accepted_at timestamptz,
 role_granted boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(conference_id,email)
);
CREATE UNIQUE INDEX event_team_token_idx ON event_team_members(token_hash) WHERE token_hash <> '';
ALTER TABLE work_shifts ADD COLUMN generation_key text, ADD COLUMN generation_hash text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX work_shifts_generation_idx ON work_shifts(conference_id,generation_key) WHERE generation_key IS NOT NULL;

ALTER TABLE conference_days ADD COLUMN event_plan_managed boolean NOT NULL DEFAULT false;
