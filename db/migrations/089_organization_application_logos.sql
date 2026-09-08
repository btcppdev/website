ALTER TABLE organization_applications
ADD COLUMN logo_light_url text NOT NULL DEFAULT '',
ADD COLUMN logo_dark_url text NOT NULL DEFAULT '';
