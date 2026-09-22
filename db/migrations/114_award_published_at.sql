-- First availability is distinct from creation (drafts) and later edits.
ALTER TABLE awards ADD COLUMN published_at timestamptz;

-- Older organizer-created awards have no publication history. Use creation
-- time as the best available fallback, rather than announcing them all today.
UPDATE awards a SET published_at = coalesce(
  (SELECT min(p.reviewed_at) FROM sponsor_award_proposals p
   WHERE p.award_id = a.id AND p.status = 'approved'), a.created_at)
WHERE a.status IN ('available', 'unawarded', 'awarded');

CREATE FUNCTION set_award_first_published_at() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.published_at IS NOT NULL THEN
    NEW.published_at := OLD.published_at;
  ELSIF NEW.status IN ('available', 'unawarded', 'awarded') THEN
    NEW.published_at := now();
  ELSE
    NEW.published_at := NULL;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER awards_first_published_at
BEFORE INSERT OR UPDATE ON awards
FOR EACH ROW EXECUTE FUNCTION set_award_first_published_at();

CREATE INDEX awards_published_at_idx ON awards (published_at)
WHERE archived_at IS NULL AND award_type = 'challenge';
