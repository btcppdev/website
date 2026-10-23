-- Only explicitly reviewed legal versions may participate in automatic forfeiture.
-- No policy is seeded: do not retroactively apply this rule to old bookings.
CREATE TABLE expedition_lifecycle_policies (
 product_code text NOT NULL DEFAULT 'expedition.antarctica2028.passage' CHECK(product_code='expedition.antarctica2028.passage'),
 version text PRIMARY KEY,
 time_zone text NOT NULL CHECK(time_zone IN ('America/Chicago','America/Argentina/Ushuaia','UTC')),
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(product_code,version) REFERENCES service_legal_revisions(product_code,version)
);
ALTER TABLE expedition_reservations DROP CONSTRAINT expedition_reservations_state_check;
ALTER TABLE expedition_reservations ADD CHECK(state IN ('held','confirmed','expired','review','abandoned','cancelled'));
ALTER TABLE expedition_reservations ADD COLUMN released_at timestamptz;
CREATE TABLE expedition_lifecycle_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 reservation_id uuid NOT NULL REFERENCES expedition_reservations(id),
 installment integer NOT NULL CHECK(installment BETWEEN 2 AND 5),
 kind text NOT NULL CHECK(kind IN ('reminder','abandoned')),
 offset_days integer NOT NULL,
 policy_version text NOT NULL REFERENCES expedition_lifecycle_policies(version),
 occurred_at timestamptz NOT NULL,
 snapshot jsonb NOT NULL,
 UNIQUE(reservation_id,installment,kind,offset_days)
);
CREATE UNIQUE INDEX expedition_abandoned_once ON expedition_lifecycle_events(reservation_id) WHERE kind='abandoned';
CREATE TABLE expedition_lifecycle_mail (
 event_id uuid NOT NULL REFERENCES expedition_lifecycle_events(id),
 audience text NOT NULL CHECK(audience IN ('customer','staff')),
 recipient text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','suppressed','review')),
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz NOT NULL DEFAULT '-infinity',
 accepted_at timestamptz,
 PRIMARY KEY(event_id,audience)
);
CREATE INDEX expedition_lifecycle_mail_due ON expedition_lifecycle_mail(next_attempt_at) WHERE state='pending';
-- Authenticated provider observations used to distinguish a terminal expired
-- invoice from a payment which may still settle. Never inferred from browser URLs.
CREATE TABLE expedition_payment_checks (
 attempt_id uuid PRIMARY KEY REFERENCES service_payment_attempts(id),
 checked_at timestamptz NOT NULL,
 status text NOT NULL,
 evidence jsonb NOT NULL
);
