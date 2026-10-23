CREATE TABLE alerts_prepaid_reminders (
 person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
 source_key text NOT NULL,
 window_days integer NOT NULL CHECK(window_days IN (7,1)),
 recipient text NOT NULL,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 accepted_at timestamptz,
 PRIMARY KEY(person_id,source_key,window_days)
);
