ALTER TABLE conferences
  ADD COLUMN IF NOT EXISTS map_latitude double precision NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS map_longitude double precision NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS map_x_percent double precision NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS map_y_percent double precision NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS map_label text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS map_label_side text NOT NULL DEFAULT '';
