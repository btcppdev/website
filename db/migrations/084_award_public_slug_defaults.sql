CREATE OR REPLACE FUNCTION set_award_public_slug()
RETURNS trigger AS $$
DECLARE
  base_slug text;
  candidate text;
  suffix integer := 1;
BEGIN
  IF nullif(btrim(NEW.public_slug), '') IS NOT NULL THEN
    RETURN NEW;
  END IF;

  -- Some imports and fixture upserts write awards directly. Serialize slug
  -- allocation per competition so those paths remain safe under concurrency.
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.competition_id::text, 0));

  base_slug := trim(both '-' FROM regexp_replace(lower(NEW.title), '[^a-z0-9]+', '-', 'g'));
  IF base_slug = '' THEN
    base_slug := 'award';
  END IF;
  candidate := base_slug;

  WHILE EXISTS (
    SELECT 1
    FROM awards
    WHERE competition_id = NEW.competition_id
      AND public_slug = candidate
      AND id IS DISTINCT FROM NEW.id
  ) LOOP
    suffix := suffix + 1;
    candidate := base_slug || '-' || suffix::text;
  END LOOP;

  NEW.public_slug := candidate;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER awards_set_public_slug
BEFORE INSERT OR UPDATE OF competition_id, public_slug ON awards
FOR EACH ROW EXECUTE FUNCTION set_award_public_slug();
