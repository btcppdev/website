BEGIN;

ALTER TABLE awards
ADD COLUMN finalists_only boolean NOT NULL DEFAULT false;

ALTER TABLE competitions
ADD COLUMN results_finalized_at timestamptz,
ADD COLUMN results_finalized_by uuid REFERENCES people(id) ON DELETE SET NULL;

CREATE TABLE competition_results_publication_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  action text NOT NULL,
  performed_by uuid REFERENCES people(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (action IN ('finalized', 'reopened'))
);

CREATE INDEX competition_results_publication_events_competition_idx
ON competition_results_publication_events (competition_id, created_at DESC);

ALTER TABLE competition_judge_invites
ADD COLUMN judge_types text[] NOT NULL DEFAULT ARRAY['expo', 'finals']::text[],
ADD COLUMN email citext,
ADD CONSTRAINT competition_judge_invites_judge_types_check CHECK (
  cardinality(judge_types) > 0
  AND judge_types <@ ARRAY['expo', 'finals']::text[]
);

CREATE INDEX competition_judge_invites_email_idx
ON competition_judge_invites (email)
WHERE email IS NOT NULL;

-- Repair links consumed by the old flow: it granted coordinator access and
-- then redirected to a judging page that coordinators could not open.
INSERT INTO competition_judges (competition_id, person_id, judge_type)
SELECT invites.competition_id, invites.accepted_by_person_id, roles.judge_type
FROM competition_judge_invites invites
CROSS JOIN LATERAL unnest(invites.judge_types) AS roles(judge_type)
WHERE invites.accepted_at IS NOT NULL
  AND invites.accepted_by_person_id IS NOT NULL
ON CONFLICT (competition_id, person_id, judge_type) DO NOTHING;

DELETE FROM competition_judges judges
USING competition_judge_invites invites
WHERE invites.competition_id = judges.competition_id
  AND invites.accepted_by_person_id = judges.person_id
  AND judges.judge_type = 'coordinator'
  AND invites.accepted_at IS NOT NULL
  AND judges.created_at BETWEEN invites.accepted_at - interval '1 minute'
                            AND invites.accepted_at + interval '1 minute';

-- Preserve already-public winners for competitions that were clearly completed
-- before this explicit publication state existed. Active and upcoming events
-- remain unpublished until a coordinator finalizes them.
UPDATE competitions
SET results_finalized_at = coalesce(awards_ceremony_at, updated_at)
WHERE results_finalized_at IS NULL
  AND (
    lifecycle_override = 'closed'
    OR (awards_ceremony_at IS NOT NULL AND awards_ceremony_at <= now())
  )
  AND EXISTS (
    SELECT 1
    FROM awards
    JOIN project_awards ON project_awards.award_id = awards.id
    WHERE awards.competition_id = competitions.id
  );

INSERT INTO competition_results_publication_events (
  competition_id, action, performed_by, created_at
)
SELECT competitions.id, 'finalized', competitions.results_finalized_by,
  competitions.results_finalized_at
FROM competitions
WHERE competitions.results_finalized_at IS NOT NULL
  AND NOT EXISTS (
    SELECT 1
    FROM competition_results_publication_events events
    WHERE events.competition_id = competitions.id
      AND events.action = 'finalized'
      AND events.created_at = competitions.results_finalized_at
  );

ALTER TABLE people
ADD COLUMN lightning_address text NOT NULL DEFAULT '',
ADD COLUMN bitcoin_address text NOT NULL DEFAULT '',
ADD COLUMN tax_form_type text NOT NULL DEFAULT '',
ADD COLUMN tax_form_object_key text NOT NULL DEFAULT '',
ADD COLUMN tax_form_original_name text NOT NULL DEFAULT '',
ADD COLUMN tax_form_uploaded_at timestamptz,
ADD CONSTRAINT people_tax_form_type_check
  CHECK (tax_form_type IN ('', 'w9', 'w8ben'));

CREATE TABLE award_distributions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  competition_id uuid NOT NULL REFERENCES competitions(id) ON DELETE CASCADE,
  award_id uuid NOT NULL REFERENCES awards(id) ON DELETE CASCADE,
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  prize_id uuid NOT NULL REFERENCES prizes(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE RESTRICT,
  distribution_type text NOT NULL,
  amount_sats bigint,
  ticket_quantity integer,
  status text NOT NULL DEFAULT 'pending',
  notes text NOT NULL DEFAULT '',
  completed_at timestamptz,
  completed_by uuid REFERENCES people(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (award_id, project_id, prize_id, person_id),
  CHECK (distribution_type IN ('sats', 'tickets', 'in_kind', 'pooled', 'trophy')),
  CHECK (status IN ('pending', 'ready', 'sent', 'claimed', 'cancelled')),
  CHECK (amount_sats IS NULL OR amount_sats > 0),
  CHECK (ticket_quantity IS NULL OR ticket_quantity > 0)
);

CREATE INDEX award_distributions_competition_status_idx
ON award_distributions (competition_id, status, created_at);

CREATE INDEX award_distributions_person_idx
ON award_distributions (person_id, status);

CREATE TRIGGER award_distributions_set_updated_at
BEFORE UPDATE ON award_distributions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE hackathon_ticket_entitlements (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  award_distribution_id uuid NOT NULL UNIQUE REFERENCES award_distributions(id) ON DELETE CASCADE,
  quantity integer NOT NULL DEFAULT 1,
  claimed_conference_id uuid REFERENCES conferences(id) ON DELETE SET NULL,
  claimed_registration_id uuid REFERENCES registrations(id) ON DELETE SET NULL,
  claimed_at timestamptz,
  voided_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (quantity > 0),
  CHECK (claimed_at IS NULL OR claimed_conference_id IS NOT NULL),
  CHECK (NOT (claimed_at IS NOT NULL AND voided_at IS NOT NULL))
);

CREATE INDEX hackathon_ticket_entitlements_person_idx
ON hackathon_ticket_entitlements (person_id, claimed_at, voided_at);

-- Reconcile ticket prizes for competitions that were finalized before ticket
-- entitlements became automatic. Each listed member receives one entitlement
-- for every ticket prize attached to every award their project won.
INSERT INTO award_distributions (
  competition_id, award_id, project_id, prize_id, person_id,
  distribution_type, ticket_quantity, status, notes
)
SELECT awards.competition_id, project_awards.award_id,
  project_awards.project_id, prizes.id, project_members.person_id,
  'tickets', 1, 'pending', 'Automatically issued when hackathon results were finalized.'
FROM project_awards
JOIN awards ON awards.id = project_awards.award_id
JOIN competitions ON competitions.id = awards.competition_id
JOIN prizes ON prizes.award_id = awards.id AND prizes.prize_type = 'tickets'
JOIN project_members ON project_members.project_id = project_awards.project_id
WHERE competitions.results_finalized_at IS NOT NULL
  AND awards.archived_at IS NULL
ON CONFLICT (award_id, project_id, prize_id, person_id) DO NOTHING;

INSERT INTO hackathon_ticket_entitlements (
  person_id, award_distribution_id, quantity
)
SELECT distributions.person_id, distributions.id,
  coalesce(distributions.ticket_quantity, 1)
FROM award_distributions distributions
JOIN competitions ON competitions.id = distributions.competition_id
JOIN prizes ON prizes.id = distributions.prize_id
WHERE competitions.results_finalized_at IS NOT NULL
  AND distributions.distribution_type = 'tickets'
  AND prizes.prize_type = 'tickets'
ON CONFLICT (award_distribution_id) DO NOTHING;

-- A person may participate in several hackathons, but only one project in any
-- individual competition. An advisory transaction lock makes the cross-table
-- check safe when two invitations or project creations race each other.
CREATE OR REPLACE FUNCTION enforce_one_project_per_hackathon()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  target_competition_id uuid;
BEGIN
  SELECT competition_id
  INTO target_competition_id
  FROM projects
  WHERE id = NEW.project_id;

  IF target_competition_id IS NULL THEN
    RETURN NEW;
  END IF;

  PERFORM pg_advisory_xact_lock(
    hashtextextended(target_competition_id::text || ':' || NEW.person_id::text, 0)
  );

  IF EXISTS (
    SELECT 1
    FROM project_members existing_membership
    JOIN projects existing_project ON existing_project.id = existing_membership.project_id
    WHERE existing_membership.person_id = NEW.person_id
      AND existing_project.competition_id = target_competition_id
      AND existing_membership.project_id <> NEW.project_id
  ) THEN
    RAISE EXCEPTION 'a person may only belong to one project per hackathon'
      USING ERRCODE = '23514',
            CONSTRAINT = 'project_members_one_project_per_hackathon';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER project_members_one_project_per_hackathon
BEFORE INSERT OR UPDATE OF project_id, person_id ON project_members
FOR EACH ROW EXECUTE FUNCTION enforce_one_project_per_hackathon();

COMMIT;
