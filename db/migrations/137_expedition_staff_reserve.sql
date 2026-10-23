-- A flexible expedition-wide reserve until staff cabin assignments are known.
CREATE TABLE expedition_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    staff_berths integer NOT NULL DEFAULT 3 CHECK (staff_berths BETWEEN 0 AND 25)
);
INSERT INTO expedition_settings(singleton,staff_berths) VALUES(true,3);
