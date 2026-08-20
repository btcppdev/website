CREATE TABLE oauth_clients (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  client_id text NOT NULL UNIQUE,
  client_secret_hash bytea,
  name text NOT NULL,
  redirect_uris text[] NOT NULL,
  allowed_scopes text[] NOT NULL,
  token_endpoint_auth_method text NOT NULL DEFAULT 'none'
    CHECK (token_endpoint_auth_method IN ('none', 'client_secret_basic')),
  created_by_person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  CHECK (cardinality(redirect_uris) > 0),
  CHECK (cardinality(allowed_scopes) > 0),
  CHECK ((token_endpoint_auth_method = 'none' AND client_secret_hash IS NULL)
    OR (token_endpoint_auth_method = 'client_secret_basic' AND client_secret_hash IS NOT NULL))
);

CREATE TABLE oauth_consents (
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  client_id uuid NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  scopes text[] NOT NULL,
  granted_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  PRIMARY KEY (person_id, client_id)
);

CREATE TABLE oauth_authorization_codes (
  code_hash bytea PRIMARY KEY,
  client_id uuid NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  redirect_uri text NOT NULL,
  scopes text[] NOT NULL,
  code_challenge text NOT NULL,
  code_challenge_method text NOT NULL CHECK (code_challenge_method = 'S256'),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz
);
CREATE INDEX oauth_authorization_codes_expiry_idx ON oauth_authorization_codes (expires_at);

CREATE TABLE oauth_access_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  selector text NOT NULL UNIQUE,
  token_hash bytea NOT NULL,
  client_id uuid NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  refresh_family_id uuid,
  scopes text[] NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  last_used_at timestamptz,
  revoked_at timestamptz
);
CREATE INDEX oauth_access_tokens_person_idx ON oauth_access_tokens (person_id, created_at DESC);
CREATE INDEX oauth_access_tokens_refresh_family_idx ON oauth_access_tokens (refresh_family_id) WHERE refresh_family_id IS NOT NULL;
CREATE INDEX oauth_access_tokens_expiry_idx ON oauth_access_tokens (expires_at);

CREATE TABLE oauth_refresh_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  token_hash bytea NOT NULL UNIQUE,
  family_id uuid NOT NULL,
  client_id uuid NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  scopes text[] NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  revoked_at timestamptz,
  replaced_by_id uuid REFERENCES oauth_refresh_tokens(id) ON DELETE SET NULL
);
CREATE INDEX oauth_refresh_tokens_family_idx ON oauth_refresh_tokens (family_id);
CREATE INDEX oauth_refresh_tokens_expiry_idx ON oauth_refresh_tokens (expires_at);
