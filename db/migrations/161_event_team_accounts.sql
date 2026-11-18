-- Existing accounts can join a planning team without an email invitation.
ALTER TABLE event_team_members ALTER COLUMN email DROP NOT NULL;
CREATE UNIQUE INDEX event_team_person_idx ON event_team_members(conference_id,person_id) WHERE person_id IS NOT NULL;
