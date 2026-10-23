CREATE TABLE site_features (
 name text PRIMARY KEY,
 enabled boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT now(),
 updated_by uuid REFERENCES people(id) ON DELETE SET NULL
);
INSERT INTO site_features(name,enabled) VALUES ('insider',false);
