CREATE TABLE newsletter_consent_requests (
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  request_key text NOT NULL,
  email citext NOT NULL,
  newsletter text NOT NULL CHECK(newsletter IN ('newsletter','insider')),
  subscribed boolean NOT NULL,
  source_client text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(person_id,request_key),
  CHECK(length(request_key) BETWEEN 16 AND 128)
);
