-- Guest-requested cancellations are distinct from automatic abandonment.
CREATE TABLE expedition_cancellations (
 reservation_id uuid PRIMARY KEY REFERENCES expedition_reservations(id),
 actor_id uuid NOT NULL REFERENCES people(id),
 cancelled_at timestamptz NOT NULL DEFAULT now(),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 1000),
 snapshot jsonb NOT NULL
);
CREATE TRIGGER expedition_cancellation_immutable BEFORE UPDATE OR DELETE ON expedition_cancellations
 FOR EACH ROW EXECUTE FUNCTION protect_service_payment_event();
