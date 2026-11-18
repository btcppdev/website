-- Publication intent is independent of the provider's current visibility.
-- A private YouTube video may be awaiting release or intentionally withheld.
CREATE TABLE recording_publication_preferences (
 recording_id uuid NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
 channel_id text NOT NULL REFERENCES publishing_channels(id) CHECK(channel_id IN ('youtube','broadcasts')),
 excluded boolean NOT NULL DEFAULT true,
 version integer NOT NULL DEFAULT 1,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(recording_id,channel_id)
);
