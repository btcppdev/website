-- A Bitcoin++ grant belongs to a private person record whether or not that
-- person currently publishes a /whois profile. The URL is optional
-- presentation metadata, not the credential subject identity.
ALTER TABLE organization_badge_grants
  ALTER COLUMN subject_profile_url SET DEFAULT '';

ALTER TABLE organization_badge_grants
  DROP CONSTRAINT IF EXISTS organization_badge_grants_subject_profile_url_check;

ALTER TABLE organization_badge_grants
  ADD CONSTRAINT organization_badge_grants_subject_profile_url_check
  CHECK (
    subject_profile_url = '' OR
    subject_profile_url ~ '^https://' OR
    subject_profile_url ~ '^http://(localhost|127\.[0-9.]+|\[::1\])([:/]|$)'
  );
