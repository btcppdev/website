ALTER TABLE registrations
  ADD COLUMN booklet_choice text NOT NULL DEFAULT '' CHECK (booklet_choice IN ('','named','private')),
  ADD COLUMN booklet_name text NOT NULL DEFAULT '' CHECK (length(booklet_name) <= 100),
  ADD COLUMN booklet_use_avatar boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT registrations_booklet_supporter CHECK (booklet_choice = '' OR type = 'supporter'),
  ADD CONSTRAINT registrations_booklet_details CHECK (
    (booklet_choice = 'named' AND type = 'supporter' AND length(btrim(booklet_name)) > 0)
    OR (booklet_choice <> 'named' AND booklet_name = '' AND NOT booklet_use_avatar)
  ),
  ADD CONSTRAINT registrations_booklet_avatar CHECK (NOT booklet_use_avatar OR attendee_is_buyer);

-- Runs after registrations_reset_attendee_details. Its rotated access key is
-- the ownership/revocation signal, so acknowledgements follow the same rules.
CREATE FUNCTION reset_supporter_acknowledgement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.attendee_access_key IS DISTINCT FROM OLD.attendee_access_key
     OR NEW.type <> 'supporter' THEN
    NEW.booklet_choice := '';
    NEW.booklet_name := '';
    NEW.booklet_use_avatar := false;
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER registrations_reset_supporter_acknowledgement
  BEFORE UPDATE ON registrations FOR EACH ROW EXECUTE FUNCTION reset_supporter_acknowledgement();
