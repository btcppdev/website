-- One lifetime test booking, tied to the verified allowlisted account and the
-- real reservation. A failed/expired checkout reuses its original request key.
CREATE TABLE expedition_beta_claims (
 email text PRIMARY KEY CHECK(email='nifty@btcpp.dev'),
 buyer_id uuid NOT NULL REFERENCES people(id),
 reservation_id uuid NOT NULL UNIQUE REFERENCES expedition_reservations(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
