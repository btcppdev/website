UPDATE conferences
SET map_latitude = v.lat,
    map_longitude = v.lon,
    map_label = CASE WHEN conferences.map_label = '' THEN v.label ELSE conferences.map_label END,
    map_label_side = CASE WHEN conferences.map_label_side = '' THEN v.side ELSE conferences.map_label_side END
FROM (VALUES
  ('atx22', 30.2672, -97.7431, 'Austin', 'right'),
  ('atx23', 30.2672, -97.7431, 'Austin', 'right'),
  ('atx24', 30.2672, -97.7431, 'Austin', 'right'),
  ('atx25', 30.2672, -97.7431, 'Austin', 'right'),
  ('ba24', -34.6037, -58.3816, 'Buenos Aires', 'right'),
  ('berlin23', 52.5200, 13.4050, 'Berlin', 'right'),
  ('berlin24', 52.5200, 13.4050, 'Berlin', 'right'),
  ('berlin25', 52.5200, 13.4050, 'Berlin', 'right'),
  ('berlin26', 52.5200, 13.4050, 'Berlin', 'right'),
  ('cdmx22', 19.4326, -99.1332, 'Mexico City', 'right'),
  ('durham', 35.9940, -78.8986, 'Durham', 'right'),
  ('floripa', -27.5949, -48.5482, 'Florianopolis', 'right'),
  ('floripa26', -27.5949, -48.5482, 'Florianopolis', 'right'),
  ('istanbul', 41.0082, 28.9784, 'Istanbul', 'right'),
  ('nairobi', -1.2864, 36.8172, 'Nairobi', 'right'),
  ('riga', 56.9496, 24.1052, 'Riga', 'right'),
  ('seoul', 37.5665, 126.9780, 'Seoul', 'left'),
  ('taipei', 25.0330, 121.5654, 'Taipei', 'left'),
  ('toronto', 43.6532, -79.3832, 'Toronto', 'right'),
  ('vegas', 36.1699, -115.1398, 'Las Vegas', 'right'),
  ('vienna', 48.2082, 16.3738, 'Vienna', 'right')
) AS v(tag, lat, lon, label, side)
WHERE conferences.tag = v.tag
  AND conferences.map_latitude = 0
  AND conferences.map_longitude = 0;
