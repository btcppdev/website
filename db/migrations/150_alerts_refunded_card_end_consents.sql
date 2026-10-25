-- Account-owned confirmation before ending a refunded card plan early.
CREATE TABLE alerts_refunded_card_end_consents (
 checkout_id uuid NOT NULL REFERENCES service_stripe_checkouts(id),
 quote_id uuid NOT NULL REFERENCES service_checkout_quotes(id),
 person_id uuid REFERENCES people(id) ON DELETE SET NULL,
 consented_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(checkout_id,quote_id)
);
