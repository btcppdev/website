ALTER TABLE registrations
ADD COLUMN IF NOT EXISTS conference_shirt_picked_up_at timestamptz,
ADD COLUMN IF NOT EXISTS conference_shirt_picked_up_by citext;
