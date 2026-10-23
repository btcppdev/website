-- Product-owned inventory and fulfillment projections only. No provider ledger.
-- No inventory is seeded: sales require explicit capacity and booking policy.
CREATE TABLE expedition_inventory (
    berth text PRIMARY KEY CHECK (berth IN ('shared','triple','quad')),
    capacity integer NOT NULL CHECK (capacity >= 0),
    sales_open boolean NOT NULL DEFAULT false,
    booking_closes_at timestamptz NOT NULL,
    hold_seconds integer NOT NULL CHECK (hold_seconds BETWEEN 60 AND 86400)
);

CREATE TABLE expedition_reservations (
    id uuid PRIMARY KEY,
    buyer_id text NOT NULL CHECK (length(buyer_id) > 0),
    request_key text NOT NULL CHECK (length(request_key) BETWEEN 16 AND 128),
    berth text NOT NULL REFERENCES expedition_inventory(berth),
    plan text NOT NULL CHECK (plan IN ('full','installments')),
    provider text NOT NULL CHECK (provider IN ('stripe','opennode')),
    state text NOT NULL CHECK (state IN ('held','confirmed','expired','review')),
    created_at timestamptz NOT NULL,
    hold_expires_at timestamptz NOT NULL,
    confirmed_at timestamptz,
    schedule jsonb NOT NULL,
    UNIQUE (buyer_id, request_key),
    CHECK (hold_expires_at > created_at)
);
CREATE INDEX expedition_occupied_berths ON expedition_reservations(berth, state, hold_expires_at);

CREATE TABLE expedition_installments (
    reservation_id uuid NOT NULL REFERENCES expedition_reservations(id),
    number integer NOT NULL CHECK (number BETWEEN 1 AND 5),
    due_at timestamptz NOT NULL,
    cents bigint NOT NULL CHECK (cents > 0),
    -- Opaque shared-commerce order identity; this is fulfillment, not a ledger.
    paid_order_id text UNIQUE,
    PRIMARY KEY (reservation_id, number)
);
