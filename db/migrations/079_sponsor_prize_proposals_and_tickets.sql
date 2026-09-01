-- Sponsor-managed benefits that require explicit accounting or organizer review.

CREATE TABLE sponsor_award_proposals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sponsorship_id uuid NOT NULL,
  conference_id uuid NOT NULL,
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  submitted_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  judging_instructions text NOT NULL DEFAULT '',
  max_awardees integer NOT NULL DEFAULT 1,
  opt_in_required boolean NOT NULL DEFAULT true,
  finalists_only boolean NOT NULL DEFAULT false,
  prize_type text NOT NULL DEFAULT 'sats',
  prize_title text NOT NULL,
  prize_description text NOT NULL DEFAULT '',
  prize_value_text text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  review_notes text NOT NULL DEFAULT '',
  reviewed_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  reviewed_at timestamptz,
  award_id uuid UNIQUE REFERENCES awards(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (sponsorship_id, conference_id)
    REFERENCES sponsorship_entitlements (sponsorship_id, conference_id)
    ON DELETE CASCADE,
  CHECK (title <> ''),
  CHECK (prize_title <> ''),
  CHECK (max_awardees > 0),
  CHECK (prize_type IN ('sats', 'in_kind', 'tickets', 'pooled', 'trophy')),
  CHECK (status IN ('pending', 'approved', 'rejected', 'withdrawn')),
  CHECK ((status = 'approved') = (award_id IS NOT NULL)),
  CHECK ((reviewed_at IS NULL) = (reviewed_by_person_id IS NULL))
);

CREATE INDEX sponsor_award_proposals_sponsorship_idx
ON sponsor_award_proposals (sponsorship_id, conference_id, created_at DESC);

CREATE INDEX sponsor_award_proposals_competition_status_idx
ON sponsor_award_proposals (competition_id, status, created_at);

CREATE TRIGGER sponsor_award_proposals_set_updated_at
BEFORE UPDATE ON sponsor_award_proposals
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE sponsor_ticket_issuances (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sponsorship_id uuid NOT NULL,
  conference_id uuid NOT NULL,
  issued_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  recipient_email citext NOT NULL,
  quantity integer NOT NULL,
  checkout_id text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (sponsorship_id, conference_id)
    REFERENCES sponsorship_entitlements (sponsorship_id, conference_id)
    ON DELETE CASCADE,
  CHECK (recipient_email <> ''),
  CHECK (quantity > 0),
  CHECK (checkout_id <> '')
);

CREATE INDEX sponsor_ticket_issuances_sponsorship_idx
ON sponsor_ticket_issuances (sponsorship_id, conference_id, created_at DESC);
