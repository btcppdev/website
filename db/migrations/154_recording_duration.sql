ALTER TABLE recordings ADD COLUMN duration_seconds bigint NOT NULL DEFAULT 0
  CHECK (duration_seconds >= 0 AND duration_seconds <= 31536000);

-- A replacement video must supply or discover its own runtime.
CREATE FUNCTION reset_recording_duration_on_media_change() RETURNS trigger AS $$
BEGIN
  IF (NEW.file_uri IS DISTINCT FROM OLD.file_uri OR NEW.youtube_url IS DISTINCT FROM OLD.youtube_url)
     AND NEW.duration_seconds IS NOT DISTINCT FROM OLD.duration_seconds THEN
    NEW.duration_seconds := 0;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER recordings_reset_duration
BEFORE UPDATE ON recordings
FOR EACH ROW EXECUTE FUNCTION reset_recording_duration_on_media_change();
