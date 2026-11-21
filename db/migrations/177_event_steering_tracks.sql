-- A public track label is separate from internal team responsibilities.
ALTER TABLE event_team_members
 ADD COLUMN track text NOT NULL DEFAULT '' CHECK (char_length(track) <= 120);
