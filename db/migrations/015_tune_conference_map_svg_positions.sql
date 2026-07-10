UPDATE conferences
SET map_x_percent = v.x,
    map_y_percent = v.y
FROM (VALUES
  ('atx22', 23.96, 31.23, 18.43, 50.13),
  ('atx23', 23.96, 31.23, 18.43, 50.13),
  ('atx24', 23.96, 31.23, 18.43, 50.13),
  ('atx25', 23.96, 31.23, 18.43, 50.13),
  ('ba24', 34.69, 71.45, 30.67, 82.08),
  ('berlin23', 53.17, 17.63, 53.00, 36.11),
  ('berlin24', 53.17, 17.63, 53.00, 36.11),
  ('berlin25', 53.17, 17.63, 53.00, 36.11),
  ('berlin26', 53.17, 17.63, 53.00, 36.11),
  ('cdmx22', 22.93, 37.95, 18.00, 55.70),
  ('durham', 29.43, 27.69, 24.29, 46.95),
  ('floripa', 36.97, 67.11, 33.73, 78.27),
  ('floripa26', 36.97, 67.11, 33.73, 78.27),
  ('istanbul', 57.38, 24.59, 57.84, 43.97),
  ('nairobi', 60.22, 50.80, 60.28, 65.51),
  ('riga', 55.50, 15.03, 56.33, 32.54),
  ('seoul', 82.87, 26.71, 88.32, 46.04),
  ('taipei', 82.85, 34.48, 86.64, 52.88),
  ('toronto', 30.09, 22.97, 24.14, 42.30),
  ('vegas', 20.01, 27.58, 13.02, 46.85),
  ('vienna', 53.99, 20.20, 53.92, 39.26)
) AS v(tag, old_x, old_y, x, y)
WHERE conferences.tag = v.tag
  AND (
    (conferences.map_x_percent = 0 AND conferences.map_y_percent = 0)
    OR (
      abs(conferences.map_x_percent - v.old_x) < 0.01
      AND abs(conferences.map_y_percent - v.old_y) < 0.01
    )
  );
