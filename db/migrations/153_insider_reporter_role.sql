-- Rename existing assignments without changing any other person roles.
INSERT INTO people_roles (person_id, scope, position)
SELECT person_id, scope, 'reporter'
FROM people_roles
WHERE scope = 'insider' AND position = 'writer'
ON CONFLICT DO NOTHING;

DELETE FROM people_roles WHERE scope = 'insider' AND position = 'writer';
