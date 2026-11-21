ALTER TABLE registrations
  ADD COLUMN attendee_name text NOT NULL DEFAULT '' CHECK (length(attendee_name) <= 100),
  ADD COLUMN attendee_is_buyer boolean NOT NULL DEFAULT false,
  ADD COLUMN attendee_tshirt text NOT NULL DEFAULT '' CHECK (attendee_tshirt IN ('','LS','LM','LL','MS','MM','ML','MXL','MXXL','MXXXL')),
  ADD COLUMN dietary_preference text NOT NULL DEFAULT '' CHECK (dietary_preference IN ('','none','vegetarian','vegan','other')),
  ADD COLUMN dietary_other text NOT NULL DEFAULT '' CHECK (length(dietary_other) <= 500),
  ADD COLUMN food_allergies text NOT NULL DEFAULT '' CHECK (length(food_allergies) <= 500),
  ADD COLUMN attendee_version bigint NOT NULL DEFAULT 0,
  ADD COLUMN attendee_access_key uuid NOT NULL DEFAULT gen_random_uuid();

-- Ownership changes must not retain the previous guest's preferences or links.
CREATE FUNCTION reset_transferred_ticket_details() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF lower(NEW.email::text) IS DISTINCT FROM lower(OLD.email::text)
     AND NEW.person_id IS NOT DISTINCT FROM OLD.person_id THEN
    NEW.person_id := (SELECT person_id FROM person_emails WHERE email=NEW.email);
  END IF;
  IF lower(NEW.email::text) IS DISTINCT FROM lower(OLD.email::text)
     OR (OLD.person_id IS NOT NULL AND NEW.person_id IS DISTINCT FROM OLD.person_id)
     OR NEW.revoked IS DISTINCT FROM OLD.revoked THEN
    NEW.attendee_name := '';
    NEW.attendee_is_buyer := false;
    NEW.attendee_tshirt := '';
    NEW.dietary_preference := '';
    NEW.dietary_other := '';
    NEW.food_allergies := '';
    NEW.attendee_version := OLD.attendee_version + 1;
    NEW.attendee_access_key := gen_random_uuid();
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER registrations_reset_attendee_details
  BEFORE UPDATE ON registrations FOR EACH ROW EXECUTE FUNCTION reset_transferred_ticket_details();

-- Legacy single tickets can still use the known profile size. Multiple tickets
-- bought together must never all inherit the purchaser's size or name.
CREATE VIEW registration_attendee_fulfillment AS
SELECT r.ref_id,
  coalesce(nullif(r.attendee_name,''), CASE WHEN r.attendee_is_buyer OR
    (r.attendee_version=0 AND (r.checkout_id='' OR NOT EXISTS (
      SELECT 1 FROM registrations sibling WHERE sibling.checkout_id=r.checkout_id
        AND sibling.conference_id=r.conference_id AND sibling.email=r.email AND sibling.ref_id<>r.ref_id AND NOT sibling.revoked
    ))) THEN p.name END, '') AS attendee_name,
  coalesce(nullif(r.attendee_tshirt,''), CASE WHEN (r.attendee_is_buyer OR
    (r.attendee_version=0 AND (r.checkout_id='' OR NOT EXISTS (
      SELECT 1 FROM registrations sibling WHERE sibling.checkout_id=r.checkout_id
        AND sibling.conference_id=r.conference_id AND sibling.email=r.email AND sibling.ref_id<>r.ref_id AND NOT sibling.revoked
    )))) AND upper(btrim(p.tshirt)) IN ('LS','LM','LL','MS','MM','ML','MXL','MXXL','MXXXL')
    THEN upper(btrim(p.tshirt)) END, '') AS tshirt
FROM registrations r LEFT JOIN people p ON p.id=r.person_id;
