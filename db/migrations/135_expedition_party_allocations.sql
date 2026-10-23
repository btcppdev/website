-- One reservation and finite schedule can cover multiple cabin arrangements.
ALTER TABLE expedition_reservations DROP CONSTRAINT expedition_reservations_quantity_check;
ALTER TABLE expedition_reservations ADD CHECK (quantity BETWEEN 1 AND 25);
CREATE TABLE expedition_allocations (
    reservation_id uuid NOT NULL REFERENCES expedition_reservations(id),
    cabin_id text NOT NULL,
    berth text NOT NULL,
    quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 4),
    preference text NOT NULL CHECK (preference IN ('male','female','coed')),
    PRIMARY KEY(reservation_id,cabin_id),
    FOREIGN KEY(cabin_id,berth) REFERENCES expedition_cabins(id,berth)
);
INSERT INTO expedition_allocations(reservation_id,cabin_id,berth,quantity,preference)
SELECT id,cabin_id,berth,quantity,sharing FROM expedition_reservations WHERE cabin_id IS NOT NULL;
CREATE INDEX expedition_allocations_category ON expedition_allocations(berth,cabin_id);
