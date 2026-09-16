-- A separate inert historical identity per deletion avoids merging unrelated teams
-- or violating one-project-per-hackathon and judge-vote uniqueness constraints.
ALTER TABLE people ADD COLUMN is_deleted_account boolean NOT NULL DEFAULT false;

CREATE FUNCTION protect_deleted_account_profile() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.is_deleted_account THEN
    RAISE EXCEPTION 'Deleted account profiles cannot be edited';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER protect_deleted_account_profile BEFORE UPDATE ON people
FOR EACH ROW EXECUTE FUNCTION protect_deleted_account_profile();

CREATE FUNCTION prevent_deleted_account_access() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM people WHERE id = NEW.person_id AND is_deleted_account) THEN
    RAISE EXCEPTION 'Deleted accounts cannot receive credentials or access';
  END IF;
  RETURN NEW;
END;
$$;
DO $$
DECLARE target text;
BEGIN
  FOREACH target IN ARRAY ARRAY['person_emails','person_email_conflicts','person_email_verifications',
    'people_roles','person_auth_security','person_password_credentials','person_nostr_credentials',
    'person_oauth_identities','person_passkey_credentials','person_api_tokens','password_reset_tokens',
    'oauth_consents','oauth_authorization_codes','oauth_access_tokens','oauth_refresh_tokens',
    'organization_memberships'] LOOP
    EXECUTE format('CREATE TRIGGER prevent_deleted_account_access BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION prevent_deleted_account_access()', target);
  END LOOP;
END;
$$;
