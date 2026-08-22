CREATE TABLE recording_broadcasts (
  recording_id uuid PRIMARY KEY REFERENCES recordings(id) ON DELETE CASCADE,
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

CREATE INDEX recording_broadcasts_live_idx
  ON recording_broadcasts (heartbeat_at DESC)
  WHERE state = 'live';
