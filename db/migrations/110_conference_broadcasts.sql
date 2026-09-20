CREATE TABLE conference_broadcasts (
  conference_id uuid PRIMARY KEY REFERENCES conferences(id) ON DELETE CASCADE,
  title text NOT NULL DEFAULT '',
  state text NOT NULL DEFAULT 'scheduled'
    CHECK (state IN ('scheduled', 'live', 'ended', 'failed')),
  hls_url text NOT NULL DEFAULT '',
  x_broadcast_url text NOT NULL DEFAULT '',
  started_at timestamptz,
  ended_at timestamptz,
  heartbeat_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (state <> 'live' OR hls_url <> '')
);
CREATE INDEX conference_broadcasts_live_idx
  ON conference_broadcasts (heartbeat_at DESC) WHERE state = 'live';
