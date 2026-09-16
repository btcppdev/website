-- NULL means the registration has not been anonymized by account deletion.
-- Preserve its historical status independently of disabling ticket access.
ALTER TABLE registrations ADD COLUMN revoked_before_account_deletion boolean;

CREATE FUNCTION protect_anonymized_registration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.revoked_before_account_deletion IS NOT NULL THEN
    NEW.person_id := OLD.person_id;
    NEW.email := OLD.email;
    NEW.revoked := true;
    NEW.revoked_before_account_deletion := OLD.revoked_before_account_deletion;
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER protect_anonymized_registration BEFORE UPDATE ON registrations
FOR EACH ROW EXECUTE FUNCTION protect_anonymized_registration();
