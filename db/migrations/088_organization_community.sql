ALTER TABLE organizations
ADD COLUMN membership_policy text NOT NULL DEFAULT 'request',
ADD CONSTRAINT organizations_membership_policy_check
CHECK (membership_policy IN ('request', 'open', 'closed'));

CREATE TABLE organization_membership_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  message text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  reviewed_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  review_note text NOT NULL DEFAULT '',
  reviewed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (status IN ('pending', 'approved', 'denied', 'cancelled'))
);

CREATE UNIQUE INDEX organization_membership_requests_one_pending_idx
ON organization_membership_requests (organization_id, person_id)
WHERE status = 'pending';

CREATE INDEX organization_membership_requests_org_status_idx
ON organization_membership_requests (organization_id, status, created_at DESC);

CREATE INDEX organization_membership_requests_person_idx
ON organization_membership_requests (person_id, created_at DESC);

CREATE TRIGGER organization_membership_requests_set_updated_at
BEFORE UPDATE ON organization_membership_requests
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE organization_applications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  submitted_by_person_id uuid NOT NULL REFERENCES people(id) ON DELETE RESTRICT,
  applicant_email citext NOT NULL,
  name text NOT NULL,
  tagline text NOT NULL DEFAULT '',
  contact_email citext,
  website_url text NOT NULL DEFAULT '',
  github_url text NOT NULL DEFAULT '',
  notes text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  review_note text NOT NULL DEFAULT '',
  reviewed_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  reviewed_at timestamptz,
  organization_id uuid REFERENCES organizations(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (applicant_email <> ''),
  CHECK (name <> ''),
  CHECK (status IN ('pending', 'approved', 'denied'))
);

CREATE UNIQUE INDEX organization_applications_one_pending_name_idx
ON organization_applications (lower(name))
WHERE status = 'pending';

CREATE INDEX organization_applications_submitter_idx
ON organization_applications (submitted_by_person_id, created_at DESC);

CREATE INDEX organization_applications_status_idx
ON organization_applications (status, created_at DESC);

CREATE TRIGGER organization_applications_set_updated_at
BEFORE UPDATE ON organization_applications
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
