BEGIN;

-- A person's verified Nostr key is a single sign-in slot and is also the
-- authoritative npub shown on their public profile. If this fails, existing
-- duplicate credentials need an explicit account-owner decision rather than
-- being silently discarded.
CREATE UNIQUE INDEX person_nostr_credentials_person_unique
ON person_nostr_credentials (person_id);

COMMIT;
