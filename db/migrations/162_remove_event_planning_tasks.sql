-- Remove the retired planning checklist, preserving other event settings.
-- Invalidate open editors so they must reload the updated plan.
UPDATE event_plans
SET data = data - 'tasks', revision = revision + 1, updated_at = now()
WHERE data ? 'tasks';
