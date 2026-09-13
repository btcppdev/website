-- Satoshi amounts have dedicated columns; they are never stored as fiat cents.
CREATE TABLE conference_pos_settings (
 conference_id uuid PRIMARY KEY REFERENCES conferences(id),
 enabled boolean NOT NULL DEFAULT false
);
CREATE TABLE conference_pos_stock (
 conference_id uuid NOT NULL REFERENCES conferences(id),
 variant_id uuid NOT NULL REFERENCES merch_variants(id),
 enabled boolean NOT NULL DEFAULT false,
 price_sats bigint NOT NULL DEFAULT 0 CHECK(price_sats BETWEEN 0 AND 500000000),
 available integer NOT NULL DEFAULT 0 CHECK(available >= 0),
 PRIMARY KEY(conference_id, variant_id)
);
CREATE TABLE conference_pos_sales (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 conference_id uuid NOT NULL REFERENCES conferences(id),
 request_id uuid NOT NULL UNIQUE,
 operator_id text NOT NULL,
 status text NOT NULL DEFAULT 'creating' CHECK(status IN ('creating','pending','paid','expired','cancelled','review')),
 total_sats bigint NOT NULL CHECK(total_sats BETWEEN 1 AND 500000000),
 currency text NOT NULL,
 local_per_btc double precision NOT NULL CHECK(local_per_btc > 0),
 charge_id text UNIQUE,
 invoice text NOT NULL DEFAULT '',
 expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 paid_at timestamptz,
 checked_at timestamptz,
 handed_over_at timestamptz
);
CREATE TABLE conference_pos_sale_items (
 sale_id uuid NOT NULL REFERENCES conference_pos_sales(id),
 variant_id uuid NOT NULL REFERENCES merch_variants(id),
 product_name text NOT NULL,
 variant_label text NOT NULL,
 quantity integer NOT NULL CHECK(quantity BETWEEN 1 AND 1000),
 price_sats bigint NOT NULL CHECK(price_sats BETWEEN 1 AND 500000000),
 PRIMARY KEY(sale_id, variant_id)
);
CREATE TABLE conference_pos_events (
 id bigserial PRIMARY KEY,
 conference_id uuid NOT NULL REFERENCES conferences(id),
 variant_id uuid REFERENCES merch_variants(id),
 sale_id uuid REFERENCES conference_pos_sales(id),
 operation_id uuid,
 actor text NOT NULL,
 description text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX conference_pos_stock_operations ON conference_pos_events(conference_id,variant_id,operation_id) WHERE operation_id IS NOT NULL;
CREATE INDEX conference_pos_sales_pending ON conference_pos_sales(created_at) WHERE status IN ('creating','pending');
