CREATE TABLE organization_member_invites (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  email citext NOT NULL,
  role text NOT NULL DEFAULT 'manager',
  token_hash text NOT NULL UNIQUE,
  invited_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  accepted_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  accepted_at timestamptz,
  revoked_at timestamptz,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (email <> ''),
  CHECK (role IN ('manager', 'member')),
  CHECK (token_hash <> '')
);

CREATE INDEX organization_member_invites_org_idx
ON organization_member_invites (organization_id, created_at DESC);

CREATE INDEX organization_member_invites_email_idx
ON organization_member_invites (email, expires_at DESC);

CREATE UNIQUE INDEX organization_member_invites_one_pending_idx
ON organization_member_invites (organization_id, email)
WHERE accepted_at IS NULL AND revoked_at IS NULL;
