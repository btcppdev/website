BEGIN;

-- A bitcoin++ person may use several OAuth providers, but only one identity
-- from any given provider. If this fails, existing duplicate rows need an
-- explicit account-owner decision rather than being silently discarded.
CREATE UNIQUE INDEX person_oauth_identities_person_provider_unique
ON person_oauth_identities (person_id, provider);

COMMIT;
