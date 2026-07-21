ALTER TABLE conference_tickets
  ADD COLUMN IF NOT EXISTS stripe_tax_code text NOT NULL DEFAULT 'txcd_00000000';

UPDATE conference_tickets
SET stripe_tax_code = 'txcd_00000000'
WHERE stripe_tax_code = '';

CREATE TABLE merch_products (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tag text NOT NULL UNIQUE,
  slug text NOT NULL UNIQUE,
  name text NOT NULL,
  subtitle text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'draft',
  product_type text NOT NULL DEFAULT 'other',
  base_price_cents integer NOT NULL DEFAULT 0,
  currency text NOT NULL DEFAULT 'USD',
  symbol text NOT NULL DEFAULT '$',
  post_symbol text NOT NULL DEFAULT '',
  stripe_tax_code text NOT NULL DEFAULT 'txcd_99999999',
  easyship_category text NOT NULL DEFAULT '',
  hs_code text NOT NULL DEFAULT '',
  country_of_origin text NOT NULL DEFAULT '',
  requires_shipping boolean NOT NULL DEFAULT true,
  allow_event_pickup boolean NOT NULL DEFAULT false,
  available_from timestamptz,
  available_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (tag ~ '^[a-z0-9][a-z0-9_-]*$'),
  CHECK (slug ~ '^[a-z0-9][a-z0-9-]*$'),
  CHECK (name <> ''),
  CHECK (status IN ('draft', 'published', 'archived')),
  CHECK (base_price_cents >= 0),
  CHECK (available_until IS NULL OR available_from IS NULL OR available_until >= available_from)
);

CREATE TRIGGER merch_products_set_updated_at
BEFORE UPDATE ON merch_products
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE merch_product_images (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id uuid NOT NULL REFERENCES merch_products(id) ON DELETE CASCADE,
  object_key text NOT NULL,
  alt_text text NOT NULL DEFAULT '',
  display_order integer NOT NULL DEFAULT 0,
  is_primary boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (object_key <> ''),
  UNIQUE (product_id, object_key)
);

CREATE UNIQUE INDEX merch_product_images_one_primary
ON merch_product_images (product_id)
WHERE is_primary;

CREATE TABLE merch_product_options (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id uuid NOT NULL REFERENCES merch_products(id) ON DELETE CASCADE,
  name text NOT NULL,
  display_order integer NOT NULL DEFAULT 0,
  required boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (name <> ''),
  UNIQUE (product_id, name)
);

CREATE TRIGGER merch_product_options_set_updated_at
BEFORE UPDATE ON merch_product_options
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE merch_product_option_values (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  option_id uuid NOT NULL REFERENCES merch_product_options(id) ON DELETE CASCADE,
  value text NOT NULL,
  display_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (value <> ''),
  UNIQUE (option_id, value)
);

CREATE TABLE merch_variants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id uuid NOT NULL REFERENCES merch_products(id) ON DELETE CASCADE,
  sku text NOT NULL UNIQUE,
  label text NOT NULL DEFAULT '',
  price_delta_cents integer NOT NULL DEFAULT 0,
  weight_grams integer NOT NULL DEFAULT 0,
  length_mm integer NOT NULL DEFAULT 0,
  width_mm integer NOT NULL DEFAULT 0,
  height_mm integer NOT NULL DEFAULT 0,
  inventory_policy text NOT NULL DEFAULT 'deny',
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (sku <> ''),
  CHECK (price_delta_cents >= 0),
  CHECK (weight_grams >= 0),
  CHECK (length_mm >= 0),
  CHECK (width_mm >= 0),
  CHECK (height_mm >= 0),
  CHECK (inventory_policy IN ('deny', 'allow_backorder', 'unlimited')),
  CHECK (status IN ('active', 'inactive', 'archived')),
  UNIQUE (product_id, sku)
);

CREATE TRIGGER merch_variants_set_updated_at
BEFORE UPDATE ON merch_variants
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE merch_variant_option_values (
  variant_id uuid NOT NULL REFERENCES merch_variants(id) ON DELETE CASCADE,
  option_value_id uuid NOT NULL REFERENCES merch_product_option_values(id) ON DELETE CASCADE,
  PRIMARY KEY (variant_id, option_value_id)
);

CREATE TABLE shop_orders (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  public_id text NOT NULL UNIQUE,
  buyer_email citext NOT NULL,
  buyer_name text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  source text NOT NULL DEFAULT 'online',
  checkout_kind text NOT NULL DEFAULT 'merch',
  payment_provider text NOT NULL DEFAULT '',
  payment_provider_id text NOT NULL DEFAULT '',
  admin_notes text NOT NULL DEFAULT '',
  currency text NOT NULL DEFAULT 'USD',
  subtotal_cents integer NOT NULL DEFAULT 0,
  discount_amount_cents integer NOT NULL DEFAULT 0,
  shipping_amount_cents integer NOT NULL DEFAULT 0,
  sales_tax_amount_cents integer NOT NULL DEFAULT 0,
  import_duty_amount_cents integer NOT NULL DEFAULT 0,
  import_tax_amount_cents integer NOT NULL DEFAULT 0,
  total_cents integer NOT NULL DEFAULT 0,
  paid_at timestamptz,
  cancelled_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (public_id <> ''),
  CHECK (status IN ('pending', 'paid', 'cancelled', 'refunded', 'partially_refunded')),
  CHECK (source IN ('online', 'pos', 'admin')),
  CHECK (checkout_kind IN ('merch', 'ticket', 'mixed')),
  CHECK (subtotal_cents >= 0),
  CHECK (discount_amount_cents >= 0),
  CHECK (shipping_amount_cents >= 0),
  CHECK (sales_tax_amount_cents >= 0),
  CHECK (import_duty_amount_cents >= 0),
  CHECK (import_tax_amount_cents >= 0),
  CHECK (total_cents >= 0)
);

CREATE TRIGGER shop_orders_set_updated_at
BEFORE UPDATE ON shop_orders
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE shop_order_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL REFERENCES shop_orders(id) ON DELETE CASCADE,
  product_id uuid REFERENCES merch_products(id) ON DELETE SET NULL,
  variant_id uuid REFERENCES merch_variants(id) ON DELETE SET NULL,
  quantity integer NOT NULL,
  fulfilled_quantity integer NOT NULL DEFAULT 0,
  refunded_quantity integer NOT NULL DEFAULT 0,
  unit_price_cents integer NOT NULL,
  discount_amount_cents integer NOT NULL DEFAULT 0,
  tax_amount_cents integer NOT NULL DEFAULT 0,
  line_total_cents integer NOT NULL,
  product_tag_snapshot text NOT NULL DEFAULT '',
  product_name_snapshot text NOT NULL,
  variant_label_snapshot text NOT NULL DEFAULT '',
  sku_snapshot text NOT NULL DEFAULT '',
  fulfillment_method text NOT NULL DEFAULT 'ship',
  sale_conference_id uuid REFERENCES conferences(id) ON DELETE SET NULL,
  pickup_conference_id uuid REFERENCES conferences(id) ON DELETE SET NULL,
  status text NOT NULL DEFAULT 'pending',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (quantity > 0),
  CHECK (fulfilled_quantity >= 0 AND fulfilled_quantity <= quantity),
  CHECK (refunded_quantity >= 0 AND refunded_quantity <= quantity),
  CHECK (unit_price_cents >= 0),
  CHECK (discount_amount_cents >= 0),
  CHECK (tax_amount_cents >= 0),
  CHECK (line_total_cents >= 0),
  CHECK (product_name_snapshot <> ''),
  CHECK (fulfillment_method IN ('ship', 'event_pickup', 'pos_takeaway')),
  CHECK (status IN ('pending', 'ready', 'fulfilled', 'cancelled', 'refunded', 'partially_refunded')),
  CHECK (fulfillment_method <> 'event_pickup' OR pickup_conference_id IS NOT NULL)
);

CREATE INDEX shop_order_items_sale_conference_idx ON shop_order_items (sale_conference_id);
CREATE INDEX shop_order_items_pickup_conference_idx ON shop_order_items (pickup_conference_id);
CREATE INDEX shop_order_items_variant_idx ON shop_order_items (variant_id);

CREATE TRIGGER shop_order_items_set_updated_at
BEFORE UPDATE ON shop_order_items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE shop_item_pickups (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_item_id uuid NOT NULL REFERENCES shop_order_items(id) ON DELETE CASCADE,
  conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
  quantity integer NOT NULL,
  picked_up_by text NOT NULL DEFAULT '',
  picked_up_at timestamptz,
  notes text NOT NULL DEFAULT '',
  CHECK (quantity > 0)
);

CREATE TABLE merch_inventory_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  variant_id uuid NOT NULL REFERENCES merch_variants(id) ON DELETE CASCADE,
  event_type text NOT NULL,
  quantity_delta integer NOT NULL,
  order_item_id uuid REFERENCES shop_order_items(id) ON DELETE SET NULL,
  conference_id uuid REFERENCES conferences(id) ON DELETE SET NULL,
  actor_email citext,
  notes text NOT NULL DEFAULT '',
  occurred_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (event_type IN ('initial', 'increase', 'decrease', 'adjustment', 'reservation', 'reservation_release', 'sale', 'refund', 'pickup', 'pos_sale'))
);

CREATE INDEX merch_inventory_events_variant_idx ON merch_inventory_events (variant_id, occurred_at);
CREATE INDEX merch_inventory_events_conference_idx ON merch_inventory_events (conference_id, occurred_at);

CREATE TABLE merch_inventory_reservations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  variant_id uuid NOT NULL REFERENCES merch_variants(id) ON DELETE CASCADE,
  checkout_session_id uuid,
  quantity integer NOT NULL,
  status text NOT NULL DEFAULT 'active',
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (quantity > 0),
  CHECK (status IN ('active', 'released', 'converted', 'expired'))
);

CREATE INDEX merch_inventory_reservations_variant_idx ON merch_inventory_reservations (variant_id, status, expires_at);

CREATE TRIGGER merch_inventory_reservations_set_updated_at
BEFORE UPDATE ON merch_inventory_reservations
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE shipping_rate_quotes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid REFERENCES shop_orders(id) ON DELETE CASCADE,
  provider text NOT NULL DEFAULT 'easyship',
  provider_quote_id text NOT NULL DEFAULT '',
  destination_country text NOT NULL DEFAULT '',
  destination_region text NOT NULL DEFAULT '',
  destination_postal_code text NOT NULL DEFAULT '',
  courier_name text NOT NULL DEFAULT '',
  service_name text NOT NULL DEFAULT '',
  amount_cents integer NOT NULL DEFAULT 0,
  currency text NOT NULL DEFAULT 'USD',
  estimated_min_days integer,
  estimated_max_days integer,
  raw_response jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (amount_cents >= 0)
);

CREATE TABLE shipments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL REFERENCES shop_orders(id) ON DELETE CASCADE,
  provider text NOT NULL DEFAULT 'easyship',
  provider_shipment_id text NOT NULL DEFAULT '',
  provider_label_id text NOT NULL DEFAULT '',
  courier_name text NOT NULL DEFAULT '',
  service_name text NOT NULL DEFAULT '',
  tracking_number text NOT NULL DEFAULT '',
  tracking_url text NOT NULL DEFAULT '',
  label_url text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  raw_response jsonb NOT NULL DEFAULT '{}'::jsonb,
  shipped_at timestamptz,
  delivered_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (status IN ('pending', 'label_created', 'shipped', 'delivered', 'returned', 'failed', 'cancelled'))
);

CREATE TRIGGER shipments_set_updated_at
BEFORE UPDATE ON shipments
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE tax_quotes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid REFERENCES shop_orders(id) ON DELETE CASCADE,
  sales_tax_provider text NOT NULL DEFAULT 'stripe',
  sales_tax_amount_cents integer NOT NULL DEFAULT 0,
  import_provider text NOT NULL DEFAULT '',
  import_duty_amount_cents integer NOT NULL DEFAULT 0,
  import_tax_amount_cents integer NOT NULL DEFAULT 0,
  incoterm text NOT NULL DEFAULT '',
  destination_country text NOT NULL DEFAULT '',
  destination_region text NOT NULL DEFAULT '',
  destination_postal_code text NOT NULL DEFAULT '',
  raw_tax_response jsonb NOT NULL DEFAULT '{}'::jsonb,
  raw_import_response jsonb NOT NULL DEFAULT '{}'::jsonb,
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (sales_tax_amount_cents >= 0),
  CHECK (import_duty_amount_cents >= 0),
  CHECK (import_tax_amount_cents >= 0),
  CHECK (incoterm IN ('', 'DDP', 'DDU'))
);

CREATE TABLE tax_transactions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL REFERENCES shop_orders(id) ON DELETE CASCADE,
  provider text NOT NULL DEFAULT 'stripe',
  provider_transaction_id text NOT NULL DEFAULT '',
  sales_tax_amount_cents integer NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'recorded',
  raw_response jsonb NOT NULL DEFAULT '{}'::jsonb,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  voided_at timestamptz,
  CHECK (sales_tax_amount_cents >= 0),
  CHECK (status IN ('recorded', 'voided', 'refunded', 'failed'))
);

CREATE TABLE refunds (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL REFERENCES shop_orders(id) ON DELETE CASCADE,
  provider text NOT NULL DEFAULT '',
  provider_refund_id text NOT NULL DEFAULT '',
  amount_cents integer NOT NULL,
  currency text NOT NULL DEFAULT 'USD',
  reason text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  requested_by text NOT NULL DEFAULT '',
  raw_response jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  CHECK (amount_cents >= 0),
  CHECK (status IN ('pending', 'succeeded', 'failed', 'cancelled'))
);

CREATE TABLE refund_items (
  refund_id uuid NOT NULL REFERENCES refunds(id) ON DELETE CASCADE,
  order_item_id uuid NOT NULL REFERENCES shop_order_items(id) ON DELETE CASCADE,
  quantity integer NOT NULL,
  amount_cents integer NOT NULL,
  restock boolean NOT NULL DEFAULT false,
  PRIMARY KEY (refund_id, order_item_id),
  CHECK (quantity > 0),
  CHECK (amount_cents >= 0)
);

CREATE TABLE shop_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  event_type text NOT NULL,
  actor_type text NOT NULL DEFAULT 'system',
  actor_email citext,
  entity_type text NOT NULL,
  entity_id uuid,
  order_id uuid REFERENCES shop_orders(id) ON DELETE SET NULL,
  order_item_id uuid REFERENCES shop_order_items(id) ON DELETE SET NULL,
  product_id uuid REFERENCES merch_products(id) ON DELETE SET NULL,
  variant_id uuid REFERENCES merch_variants(id) ON DELETE SET NULL,
  conference_id uuid REFERENCES conferences(id) ON DELETE SET NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (event_type <> ''),
  CHECK (actor_type IN ('system', 'admin', 'buyer', 'volunteer', 'pos')),
  CHECK (entity_type <> '')
);

CREATE INDEX shop_events_entity_idx ON shop_events (entity_type, entity_id, created_at);
CREATE INDEX shop_events_conference_idx ON shop_events (conference_id, created_at);
CREATE INDEX shop_events_order_idx ON shop_events (order_id, created_at);

-- Conference-specific merch add-ons for ticket checkout.
CREATE TABLE conference_merch_upsells (
  conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
  product_id uuid NOT NULL REFERENCES merch_products(id) ON DELETE CASCADE,
  display_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (conference_id, product_id),
  CHECK (display_order >= 0)
);

CREATE INDEX conference_merch_upsells_conference_order_idx
ON conference_merch_upsells (conference_id, display_order, created_at);

-- Checkout lifecycle and payment integrity.
ALTER TABLE shop_orders
ADD COLUMN checkout_expires_at timestamptz;

ALTER TABLE merch_inventory_reservations
ADD COLUMN order_item_id uuid REFERENCES shop_order_items(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX merch_inventory_reservations_order_item_idx
ON merch_inventory_reservations (order_item_id)
WHERE order_item_id IS NOT NULL;

CREATE INDEX shop_orders_pending_expiry_idx
ON shop_orders (checkout_expires_at)
WHERE status = 'pending' AND checkout_expires_at IS NOT NULL;

CREATE UNIQUE INDEX shop_orders_payment_provider_id_idx
ON shop_orders (payment_provider, payment_provider_id)
WHERE payment_provider <> '' AND payment_provider_id <> '';

CREATE TABLE shop_order_addresses (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id uuid NOT NULL REFERENCES shop_orders(id) ON DELETE CASCADE,
  address_type text NOT NULL DEFAULT 'shipping',
  name text NOT NULL DEFAULT '',
  line1 text NOT NULL DEFAULT '',
  line2 text NOT NULL DEFAULT '',
  city text NOT NULL DEFAULT '',
  region text NOT NULL DEFAULT '',
  postal_code text NOT NULL DEFAULT '',
  country text NOT NULL DEFAULT '',
  phone text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (order_id, address_type),
  CHECK (address_type IN ('shipping', 'billing'))
);

CREATE TRIGGER shop_order_addresses_set_updated_at
BEFORE UPDATE ON shop_order_addresses
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tax_quotes
  ADD COLUMN provider_quote_id text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX tax_quotes_provider_quote_id_uq
  ON tax_quotes (sales_tax_provider, provider_quote_id)
  WHERE provider_quote_id <> '';

CREATE UNIQUE INDEX refunds_provider_refund_id_uq
  ON refunds (provider, provider_refund_id)
  WHERE provider_refund_id <> '';

CREATE UNIQUE INDEX tax_transactions_provider_transaction_id_uq
  ON tax_transactions (provider, provider_transaction_id)
  WHERE provider_transaction_id <> '';

CREATE UNIQUE INDEX tax_transactions_recorded_order_uq
  ON tax_transactions (order_id, provider)
  WHERE status = 'recorded';

CREATE INDEX shop_order_items_order_idx ON shop_order_items (order_id);

-- Easyship configuration, callbacks, and idempotent shipment creation.
CREATE TABLE easyship_settings (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  contact_name text NOT NULL DEFAULT '',
  company_name text NOT NULL DEFAULT '',
  email text NOT NULL DEFAULT '',
  phone text NOT NULL DEFAULT '',
  line_1 text NOT NULL DEFAULT '',
  line_2 text NOT NULL DEFAULT '',
  city text NOT NULL DEFAULT '',
  region text NOT NULL DEFAULT '',
  postal_code text NOT NULL DEFAULT '',
  country_alpha2 text NOT NULL DEFAULT '',
  updated_by citext,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (country_alpha2 = '' OR country_alpha2 ~ '^[A-Z]{2}$')
);

CREATE TRIGGER easyship_settings_set_updated_at
BEFORE UPDATE ON easyship_settings
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE shipments
  ADD COLUMN label_state text NOT NULL DEFAULT 'not_created',
  ADD COLUMN delivery_state text NOT NULL DEFAULT 'not_created',
  ADD COLUMN last_webhook_at timestamptz,
  ADD COLUMN last_synced_at timestamptz,
  ADD COLUMN shipping_notified_at timestamptz;

CREATE UNIQUE INDEX shipments_easyship_provider_id_unique
ON shipments (provider, provider_shipment_id)
WHERE provider = 'easyship' AND provider_shipment_id <> '';

CREATE TABLE easyship_webhook_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  payload_sha256 text NOT NULL UNIQUE,
  event_type text NOT NULL,
  resource_type text NOT NULL DEFAULT '',
  resource_id text NOT NULL DEFAULT '',
  easyship_shipment_id text NOT NULL DEFAULT '',
  payload jsonb NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  attempts integer NOT NULL DEFAULT 0,
  last_error text NOT NULL DEFAULT '',
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  received_at timestamptz NOT NULL DEFAULT now(),
  processed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  CHECK (event_type <> ''),
  CHECK (status IN ('pending', 'processing', 'processed', 'failed', 'ignored')),
  CHECK (attempts >= 0)
);

CREATE INDEX easyship_webhook_events_pending_idx
ON easyship_webhook_events (next_attempt_at, received_at)
WHERE status IN ('pending', 'failed', 'processing');

CREATE TRIGGER easyship_webhook_events_set_updated_at
BEFORE UPDATE ON easyship_webhook_events
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE shipping_rate_quotes
  ADD COLUMN courier_service_id text NOT NULL DEFAULT '';

UPDATE shipping_rate_quotes
SET courier_service_id = provider_quote_id
WHERE provider = 'easyship' AND courier_service_id = '';

ALTER TABLE shipments
  ADD COLUMN courier_service_id text NOT NULL DEFAULT '',
  ADD COLUMN create_idempotency_key uuid NOT NULL DEFAULT gen_random_uuid(),
  ADD COLUMN label_idempotency_key uuid NOT NULL DEFAULT gen_random_uuid(),
  ADD COLUMN last_error text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX shipments_easyship_create_idempotency_unique
ON shipments (create_idempotency_key)
WHERE provider = 'easyship';

CREATE UNIQUE INDEX shipments_one_active_easyship_per_order
ON shipments (order_id)
WHERE provider = 'easyship' AND status <> 'cancelled';

CREATE TABLE shipment_items (
  shipment_id uuid NOT NULL REFERENCES shipments(id) ON DELETE CASCADE,
  order_item_id uuid NOT NULL REFERENCES shop_order_items(id) ON DELETE CASCADE,
  quantity integer NOT NULL,
  sku text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  value_cents integer NOT NULL DEFAULT 0,
  weight_grams integer NOT NULL DEFAULT 0,
  length_mm integer NOT NULL DEFAULT 0,
  width_mm integer NOT NULL DEFAULT 0,
  height_mm integer NOT NULL DEFAULT 0,
  hs_code text NOT NULL DEFAULT '',
  easyship_category text NOT NULL DEFAULT '',
  origin_country_alpha2 text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (shipment_id, order_item_id),
  CHECK (quantity > 0),
  CHECK (value_cents >= 0),
  CHECK (weight_grams >= 0 AND length_mm >= 0 AND width_mm >= 0 AND height_mm >= 0)
);

-- Structured event venue address used for pickup sales tax.
ALTER TABLE conferences
  ADD COLUMN pickup_address_line1 text NOT NULL DEFAULT '',
  ADD COLUMN pickup_address_line2 text NOT NULL DEFAULT '',
  ADD COLUMN pickup_address_city text NOT NULL DEFAULT '',
  ADD COLUMN pickup_address_region text NOT NULL DEFAULT '',
  ADD COLUMN pickup_address_postal_code text NOT NULL DEFAULT '',
  ADD COLUMN pickup_address_country text NOT NULL DEFAULT '';
