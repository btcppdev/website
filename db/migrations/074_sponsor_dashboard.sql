-- Sponsor dashboard authorization and event-specific benefits.
--
-- Organization membership is deliberately separate from people_roles: a
-- person may manage several organizations, and membership alone does not
-- grant access to conference administration.
CREATE TABLE organization_memberships (
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  role text NOT NULL DEFAULT 'manager',
  status text NOT NULL DEFAULT 'active',
  invited_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, person_id),
  CHECK (role IN ('owner', 'manager', 'member')),
  CHECK (status IN ('active', 'removed'))
);

CREATE INDEX organization_memberships_person_idx
ON organization_memberships (person_id, status);

CREATE TRIGGER organization_memberships_set_updated_at
BEFORE UPDATE ON organization_memberships
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Benefits are copied onto the particular sponsorship/conference agreement.
-- They must not be inferred from the mutable human-facing tier label.
CREATE TABLE sponsorship_entitlements (
  sponsorship_id uuid NOT NULL REFERENCES sponsorships(id) ON DELETE CASCADE,
  conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
  ticket_allocation integer NOT NULL DEFAULT 0,
  sponsor_award_limit integer NOT NULL DEFAULT 0,
  participant_contact_access boolean NOT NULL DEFAULT false,
  participant_contact_export boolean NOT NULL DEFAULT false,
  can_manage_award_judges boolean NOT NULL DEFAULT false,
  can_edit_organization boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (sponsorship_id, conference_id),
  CHECK (ticket_allocation >= 0),
  CHECK (sponsor_award_limit >= 0),
  CHECK (NOT participant_contact_export OR participant_contact_access)
);

ALTER TABLE sponsorship_entitlements
ADD CONSTRAINT sponsorship_entitlements_conference_link_fk
FOREIGN KEY (sponsorship_id, conference_id)
REFERENCES sponsorships_conferences (sponsorship_id, conference_id)
ON DELETE CASCADE;

CREATE TRIGGER sponsorship_entitlements_set_updated_at
BEFORE UPDATE ON sponsorship_entitlements
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Consent belongs to an individual participant. Project owners cannot grant
-- access to a teammate's address on that teammate's behalf.
CREATE TABLE hackathon_sponsor_contact_consents (
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  all_hackathon_sponsors boolean NOT NULL DEFAULT false,
  entered_award_sponsors boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (competition_id, person_id)
);

CREATE INDEX hackathon_sponsor_contact_consents_person_idx
ON hackathon_sponsor_contact_consents (person_id, updated_at DESC);

CREATE TRIGGER hackathon_sponsor_contact_consents_set_updated_at
BEFORE UPDATE ON hackathon_sponsor_contact_consents
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Consent changes are append-only so the site can establish exactly what a
-- participant granted or withdrew and which disclosure language was shown.
CREATE TABLE hackathon_sponsor_contact_consent_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  all_hackathon_sponsors boolean NOT NULL,
  entered_award_sponsors boolean NOT NULL,
  policy_version text NOT NULL,
  source text NOT NULL DEFAULT 'web',
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (policy_version <> ''),
  CHECK (source <> '')
);

CREATE INDEX hackathon_sponsor_contact_consent_events_person_idx
ON hackathon_sponsor_contact_consent_events
  (competition_id, person_id, created_at DESC);

-- Sensitive sponsor operations and future contact-data access are auditable.
CREATE TABLE sponsor_audit_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  sponsorship_id uuid REFERENCES sponsorships(id) ON DELETE SET NULL,
  conference_id uuid REFERENCES conferences(id) ON DELETE SET NULL,
  actor_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  action text NOT NULL,
  target_type text NOT NULL DEFAULT '',
  target_id text NOT NULL DEFAULT '',
  details jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (action <> '')
);

CREATE INDEX sponsor_audit_events_org_created_idx
ON sponsor_audit_events (organization_id, created_at DESC);
