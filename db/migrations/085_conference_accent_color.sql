ALTER TABLE conferences
ADD COLUMN accent_color text NOT NULL DEFAULT '#f9af5e';

ALTER TABLE conferences
ADD CONSTRAINT conferences_accent_color_format
CHECK (accent_color ~ '^#[0-9a-fA-F]{6}$');
