CREATE TABLE recording_x_broadcasts (
  recording_id uuid PRIMARY KEY REFERENCES recordings(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'creating'
    CHECK (status IN (
      'creating', 'created', 'uploading_poster', 'poster_uploaded',
      'finalizing', 'scheduled', 'failed'
    )),
  scheduled_at timestamptz NOT NULL,
  scheduled_broadcast_id text NOT NULL DEFAULT '',
  broadcast_id text NOT NULL DEFAULT '',
  poster_media_id text NOT NULL DEFAULT '',
  poster_url text NOT NULL DEFAULT '',
  x_session_id text NOT NULL,
  optimistic_poster_url text NOT NULL,
  error text NOT NULL DEFAULT '',
  operation_started_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((scheduled_broadcast_id = '') = (broadcast_id = '')),
  CHECK (poster_media_id = '' OR broadcast_id <> ''),
  CHECK (status <> 'scheduled' OR (broadcast_id <> '' AND poster_media_id <> '' AND poster_url <> ''))
);

CREATE UNIQUE INDEX recording_x_broadcasts_broadcast_id_idx
  ON recording_x_broadcasts (broadcast_id)
  WHERE broadcast_id <> '';
