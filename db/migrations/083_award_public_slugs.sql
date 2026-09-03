ALTER TABLE awards ADD COLUMN public_slug text;

DO $$
DECLARE
  award_row record;
  base_slug text;
  candidate text;
  suffix integer;
BEGIN
  FOR award_row IN
    SELECT id, competition_id, title
    FROM awards
    ORDER BY competition_id, created_at, id
  LOOP
    base_slug := trim(both '-' FROM regexp_replace(lower(award_row.title), '[^a-z0-9]+', '-', 'g'));
    IF base_slug = '' THEN
      base_slug := 'award';
    END IF;
    candidate := base_slug;
    suffix := 1;

    WHILE EXISTS (
      SELECT 1
      FROM awards
      WHERE competition_id = award_row.competition_id
        AND public_slug = candidate
    ) LOOP
      suffix := suffix + 1;
      candidate := base_slug || '-' || suffix::text;
    END LOOP;

    UPDATE awards SET public_slug = candidate WHERE id = award_row.id;
  END LOOP;
END;
$$;

ALTER TABLE awards ALTER COLUMN public_slug SET NOT NULL;
ALTER TABLE awards ADD CONSTRAINT awards_public_slug_format CHECK (public_slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$');
CREATE UNIQUE INDEX awards_competition_public_slug_idx ON awards (competition_id, public_slug);
