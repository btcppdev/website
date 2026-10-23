-- Physical allocation confirmed by the organizer. These are internal allocation
-- labels, not the operator's cabin numbers. Inventory sales settings stay closed.
CREATE TABLE expedition_cabins (
    id text PRIMARY KEY,
    berth text NOT NULL CHECK (berth IN ('shared','triple','quad')),
    capacity integer NOT NULL CHECK (capacity BETWEEN 2 AND 4),
    enabled boolean NOT NULL DEFAULT true,
    UNIQUE (id, berth)
);
INSERT INTO expedition_cabins(id,berth,capacity)
SELECT 'twin-' || n,'shared',2 FROM generate_series(1,9) n;
INSERT INTO expedition_cabins(id,berth,capacity) VALUES
    ('triple-1','triple',3), ('quad-1','quad',4);

ALTER TABLE expedition_reservations
    ADD COLUMN cabin_id text,
    ADD COLUMN quantity integer NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 4),
    ADD COLUMN sharing text CHECK (sharing IN ('male','female','coed')),
    ADD FOREIGN KEY (cabin_id,berth) REFERENCES expedition_cabins(id,berth),
    ADD CHECK ((cabin_id IS NULL) = (sharing IS NULL));
CREATE INDEX expedition_cabin_reservations ON expedition_reservations(cabin_id);
