-- Payment method changes are account-owned instructions, not access grants.
CREATE TABLE alerts_card_updates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 checkout_id uuid NOT NULL REFERENCES service_stripe_checkouts(id) ON DELETE CASCADE,
 session_id text,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','applied')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX alerts_card_update_session ON alerts_card_updates(session_id) WHERE session_id IS NOT NULL;
CREATE TABLE alerts_bitcoin_switches (
 id uuid PRIMARY KEY REFERENCES service_stripe_cancellations(id) ON DELETE CASCADE,
 checkout_id uuid NOT NULL REFERENCES service_stripe_checkouts(id) ON DELETE CASCADE,
 quantity bigint NOT NULL CHECK(quantity>0),
 period_end bigint NOT NULL,
 state text NOT NULL DEFAULT 'scheduled' CHECK(state IN ('scheduled','resuming','withdrawn')),
 purchase_id uuid REFERENCES service_purchase_requests(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX alerts_bitcoin_switch_active ON alerts_bitcoin_switches(checkout_id) WHERE state<>'withdrawn';
