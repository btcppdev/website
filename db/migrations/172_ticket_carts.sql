-- One provider payment can reserve multiple ticket types atomically. Legacy
-- reservations retain an empty snapshot and their original single-ticket path.
ALTER TABLE ticket_reservations ADD COLUMN ticket_lines jsonb NOT NULL DEFAULT '[]'::jsonb
 CHECK (jsonb_typeof(ticket_lines) = 'array');
