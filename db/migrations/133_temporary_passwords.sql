BEGIN;
ALTER TABLE person_password_credentials
 ADD COLUMN must_change_password boolean NOT NULL DEFAULT false,
 ADD COLUMN temporary_expires_at timestamptz;
ALTER TABLE person_password_credentials ADD CONSTRAINT temporary_password_expiry_required
 CHECK (NOT must_change_password OR temporary_expires_at IS NOT NULL);
COMMIT;
