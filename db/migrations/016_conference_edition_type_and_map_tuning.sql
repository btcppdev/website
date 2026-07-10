ALTER TABLE conferences
  ADD COLUMN IF NOT EXISTS edition_type text NOT NULL DEFAULT 'global';

UPDATE conferences
SET edition_type = 'global'
WHERE edition_type = '';

UPDATE conferences
SET edition_type = 'local'
WHERE tag = 'durham';

UPDATE conferences
SET map_x_percent = 14.10,
    map_y_percent = 46.85
WHERE tag = 'vegas'
  AND (
    (map_x_percent = 0 AND map_y_percent = 0)
    OR (abs(map_x_percent - 13.02) < 0.01 AND abs(map_y_percent - 46.85) < 0.01)
    OR (abs(map_x_percent - 20.01) < 0.01 AND abs(map_y_percent - 27.58) < 0.01)
  );
