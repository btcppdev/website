BEGIN;

ALTER TABLE organizations ADD COLUMN public_slug text;

WITH normalized AS (
  SELECT id,
    coalesce(nullif(
      regexp_replace(
        regexp_replace(lower(btrim(name)), '[^a-z0-9]+', '-', 'g'),
        '(^-+|-+$)', '', 'g'
      ),
      ''
    ), 'organization') AS base_slug
  FROM organizations
), ranked AS (
  SELECT id, base_slug,
    row_number() OVER (PARTITION BY base_slug ORDER BY id) AS collision_rank
  FROM normalized
)
UPDATE organizations
SET public_slug = CASE
  WHEN ranked.collision_rank = 1 THEN ranked.base_slug
  ELSE ranked.base_slug || '-' || left(replace(organizations.id::text, '-', ''), 8)
END
FROM ranked
WHERE ranked.id = organizations.id;

ALTER TABLE organizations ALTER COLUMN public_slug SET NOT NULL;
ALTER TABLE organizations ADD CONSTRAINT organizations_public_slug_format
  CHECK (public_slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$');
CREATE UNIQUE INDEX organizations_public_slug_key ON organizations (public_slug);

CREATE FUNCTION assign_organization_public_slug() RETURNS trigger AS $$
DECLARE
  base_slug text;
  candidate text;
  suffix integer := 1;
BEGIN
  IF coalesce(btrim(NEW.public_slug), '') <> '' THEN
    NEW.public_slug := lower(btrim(NEW.public_slug));
    RETURN NEW;
  END IF;

  base_slug := coalesce(nullif(
    regexp_replace(
      regexp_replace(lower(btrim(NEW.name)), '[^a-z0-9]+', '-', 'g'),
      '(^-+|-+$)', '', 'g'
    ),
    ''
  ), 'organization');
  -- Organization applications and imports can create the same name at the
  -- same time. Serialize allocation for that base so both cannot choose the
  -- same suffix before the unique index is checked.
  PERFORM pg_advisory_xact_lock(hashtextextended(base_slug, 0));
  candidate := base_slug;
  WHILE EXISTS (SELECT 1 FROM organizations WHERE public_slug = candidate) LOOP
    suffix := suffix + 1;
    candidate := base_slug || '-' || suffix::text;
  END LOOP;
  NEW.public_slug := candidate;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER organizations_assign_public_slug
BEFORE INSERT ON organizations
FOR EACH ROW EXECUTE FUNCTION assign_organization_public_slug();

COMMIT;
