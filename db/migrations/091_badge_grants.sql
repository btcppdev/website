-- Bitcoin++ owns the intent/lifecycle of organization grants. Signed Nostr
-- events remain in Badge Studio; these rows only bind a Bitcoin++ person and
-- organization to a portable badge definition and its eventual event IDs.
CREATE TABLE organization_badge_grants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  recipient_person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  created_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  issuer_pubkey text NOT NULL,
  badge_identifier text NOT NULL,
  badge_name text NOT NULL,
  badge_description text NOT NULL DEFAULT '',
  badge_image_url text NOT NULL,
  subject_profile_url text NOT NULL,
  recipient_pubkey text NOT NULL DEFAULT '',
  state text NOT NULL,
  award_event_id text NOT NULL DEFAULT '',
  acceptance_event_id text NOT NULL DEFAULT '',
  revocation_event_id text NOT NULL DEFAULT '',
  revocation_reason text NOT NULL DEFAULT '',
  delivery_error text NOT NULL DEFAULT '',
  corrected_by_grant_id uuid REFERENCES organization_badge_grants(id) ON DELETE SET NULL,
  granted_at timestamptz NOT NULL DEFAULT now(),
  ready_at timestamptz,
  issued_at timestamptz,
  accepted_at timestamptz,
  revoked_at timestamptz,
  canceled_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (issuer_pubkey ~ '^[0-9a-f]{64}$'),
  CHECK (badge_identifier <> ''),
  CHECK (badge_name <> ''),
  CHECK (badge_image_url ~ '^https://' OR badge_image_url ~ '^http://(localhost|127\.[0-9.]+|\[::1\])([:/]|$)'),
  CHECK (subject_profile_url ~ '^https://' OR subject_profile_url ~ '^http://(localhost|127\.[0-9.]+|\[::1\])([:/]|$)'),
  CHECK (recipient_pubkey = '' OR recipient_pubkey ~ '^[0-9a-f]{64}$'),
  CHECK (award_event_id = '' OR award_event_id ~ '^[0-9a-f]{64}$'),
  CHECK (acceptance_event_id = '' OR acceptance_event_id ~ '^[0-9a-f]{64}$'),
  CHECK (revocation_event_id = '' OR revocation_event_id ~ '^[0-9a-f]{64}$'),
  CHECK (state IN ('granted', 'ready_to_issue', 'issued', 'accepted', 'revoked', 'delivery_error', 'canceled', 'corrected'))
);

CREATE UNIQUE INDEX organization_badge_grants_active_unique_idx
ON organization_badge_grants (organization_id, issuer_pubkey, badge_identifier, recipient_person_id)
WHERE state NOT IN ('canceled', 'corrected');

CREATE INDEX organization_badge_grants_person_idx
ON organization_badge_grants (recipient_person_id, granted_at DESC);

CREATE INDEX organization_badge_grants_organization_idx
ON organization_badge_grants (organization_id, granted_at DESC);

CREATE TRIGGER organization_badge_grants_set_updated_at
BEFORE UPDATE ON organization_badge_grants
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
