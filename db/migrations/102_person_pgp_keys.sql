CREATE TABLE person_pgp_keys (
    person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    fingerprint text NOT NULL CHECK (fingerprint ~ '^([0-9A-F]{40}|[0-9A-F]{64})$'),
    key_id text NOT NULL CHECK (key_id ~ '^[0-9A-F]{16}$'),
    public_key text NOT NULL CHECK (octet_length(public_key) <= 131072),
    verified_at timestamptz,
    challenge text NOT NULL DEFAULT '',
    challenge_expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (person_id, fingerprint),
    CHECK ((challenge = '') = (challenge_expires_at IS NULL)),
    CHECK (verified_at IS NULL OR challenge = '')
);
