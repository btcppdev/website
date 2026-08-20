BEGIN;

-- Profile Nostr values were copied into this table before they had been
-- proven by a signature. They remain public profile metadata, but must be
-- explicitly linked before they can be used to sign in.
DELETE FROM person_nostr_credentials
WHERE pubkey_hex IS NULL;

COMMIT;
