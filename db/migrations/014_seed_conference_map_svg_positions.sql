UPDATE conferences
SET map_x_percent = v.x,
    map_y_percent = v.y,
    map_label = CASE WHEN conferences.map_label = '' THEN v.label ELSE conferences.map_label END,
    map_label_side = CASE WHEN conferences.map_label_side = '' THEN v.side ELSE conferences.map_label_side END
FROM (VALUES
  ('atx22', 23.96, 31.23, 'Austin', 'right'),
  ('atx23', 23.96, 31.23, 'Austin', 'right'),
  ('atx24', 23.96, 31.23, 'Austin', 'right'),
  ('atx25', 23.96, 31.23, 'Austin', 'right'),
  ('ba24', 34.69, 71.45, 'Buenos Aires', 'right'),
  ('berlin23', 53.17, 17.63, 'Berlin', 'right'),
  ('berlin24', 53.17, 17.63, 'Berlin', 'right'),
  ('berlin25', 53.17, 17.63, 'Berlin', 'right'),
  ('berlin26', 53.17, 17.63, 'Berlin', 'right'),
  ('cdmx22', 22.93, 37.95, 'Mexico City', 'right'),
  ('durham', 29.43, 27.69, 'Durham', 'right'),
  ('floripa', 36.97, 67.11, 'Florianopolis', 'right'),
  ('floripa26', 36.97, 67.11, 'Florianopolis', 'right'),
  ('istanbul', 57.38, 24.59, 'Istanbul', 'right'),
  ('nairobi', 60.22, 50.80, 'Nairobi', 'right'),
  ('riga', 55.50, 15.03, 'Riga', 'right'),
  ('seoul', 82.87, 26.71, 'Seoul', 'left'),
  ('taipei', 82.85, 34.48, 'Taipei', 'left'),
  ('toronto', 30.09, 22.97, 'Toronto', 'right'),
  ('vegas', 20.01, 27.58, 'Las Vegas', 'right'),
  ('vienna', 53.99, 20.20, 'Vienna', 'right')
) AS v(tag, x, y, label, side)
WHERE conferences.tag = v.tag
  AND conferences.map_x_percent = 0
  AND conferences.map_y_percent = 0;
