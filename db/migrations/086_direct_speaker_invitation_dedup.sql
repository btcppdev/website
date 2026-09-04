-- Identify organizer-originated invitations so one person cannot receive
-- multiple unresolved direct invitations for the same conference. Multiple
-- ordinary talk proposals remain valid and intentionally unrestricted.
ALTER TABLE proposals
ADD COLUMN direct_invitee_person_id uuid REFERENCES people(id) ON DELETE SET NULL;

-- Adopt one existing placeholder invitation per person/event. Historical
-- duplicates remain readable but do not prevent this migration from adding
-- the uniqueness guard.
WITH single_speaker_invites AS (
  SELECT proposals.id, proposals.conference_id, min(speaker_confs.speaker_id::text)::uuid AS speaker_id
  FROM proposals
  JOIN proposals_speaker_confs ON proposals_speaker_confs.proposal_id = proposals.id
  JOIN speaker_confs ON speaker_confs.id = proposals_speaker_confs.speaker_conf_id
  WHERE proposals.status = 'Invited' AND proposals.title LIKE 'TBD (%'
  GROUP BY proposals.id, proposals.conference_id
  HAVING count(DISTINCT speaker_confs.speaker_id) = 1
), ranked AS (
  SELECT single_speaker_invites.*,
    row_number() OVER (
      PARTITION BY conference_id, speaker_id
      ORDER BY id
    ) AS invitation_number
  FROM single_speaker_invites
)
UPDATE proposals
SET direct_invitee_person_id = ranked.speaker_id
FROM ranked
WHERE proposals.id = ranked.id AND ranked.invitation_number = 1;

CREATE UNIQUE INDEX proposals_one_pending_direct_invite_idx
ON proposals (conference_id, direct_invitee_person_id)
WHERE status = 'Invited' AND direct_invitee_person_id IS NOT NULL;
