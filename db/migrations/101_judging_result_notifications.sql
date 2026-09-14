-- Transactional notifications wake SSE viewers on every web instance. No scores
-- or personal information are sent through the notification channel.
CREATE FUNCTION notify_judging_results_changed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  -- Timeline reads currently synchronize judge-event metadata. Ignore no-op
  -- writes (including updated_at-only changes), or readers would notify themselves.
  IF TG_OP = 'UPDATE' AND (to_jsonb(NEW) - 'updated_at') IS NOT DISTINCT FROM (to_jsonb(OLD) - 'updated_at') THEN
    RETURN NULL;
  END IF;
  PERFORM pg_notify('btcpp_judging_results', 'changed');
  RETURN NULL;
END;
$$;

-- Row triggers ignore no-op writes. PostgreSQL coalesces identical notifications
-- within each transaction and delivers them only after commit.
CREATE TRIGGER judging_results_scorecards AFTER INSERT OR UPDATE OR DELETE ON scorecards
FOR EACH ROW EXECUTE FUNCTION notify_judging_results_changed();
CREATE TRIGGER judging_results_deliberation AFTER INSERT OR UPDATE OR DELETE ON judge_event_deliberations
FOR EACH ROW EXECUTE FUNCTION notify_judging_results_changed();
CREATE TRIGGER judging_results_events AFTER INSERT OR UPDATE OR DELETE ON judge_events
FOR EACH ROW EXECUTE FUNCTION notify_judging_results_changed();
CREATE TRIGGER judging_results_projects AFTER INSERT OR UPDATE OR DELETE ON projects
FOR EACH ROW EXECUTE FUNCTION notify_judging_results_changed();
CREATE TRIGGER judging_results_competitions AFTER INSERT OR UPDATE OR DELETE ON competitions
FOR EACH ROW EXECUTE FUNCTION notify_judging_results_changed();
CREATE TRIGGER judging_results_judges AFTER INSERT OR UPDATE OR DELETE ON competition_judges
FOR EACH ROW EXECUTE FUNCTION notify_judging_results_changed();
CREATE TRIGGER judging_results_roles AFTER INSERT OR UPDATE OR DELETE ON people_roles
FOR EACH ROW EXECUTE FUNCTION notify_judging_results_changed();
