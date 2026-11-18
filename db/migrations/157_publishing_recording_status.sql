-- Cache provider visibility against the exact video identity. Replacing a
-- video automatically invalidates the old cached status at the join boundary.
CREATE TABLE recording_youtube_status (
 recording_id uuid PRIMARY KEY REFERENCES recordings(id) ON DELETE CASCADE,
 youtube_url text NOT NULL,
 privacy_status text NOT NULL CHECK(privacy_status IN ('private','unlisted','public')),
 checked_at timestamptz NOT NULL DEFAULT now()
);
