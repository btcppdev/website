ALTER TABLE conferences
ADD COLUMN IF NOT EXISTS youtube_playlist_id text NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS youtube_playlist_title text NOT NULL DEFAULT '';
