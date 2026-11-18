CREATE TABLE organization_shop_profiles (
 organization_id uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE RESTRICT,
 data jsonb NOT NULL DEFAULT '{}',
 revision bigint NOT NULL DEFAULT 0,
 updated_by uuid REFERENCES people(id) ON DELETE SET NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
-- bitcoin++ remains the seller. This organization is responsible for fulfillment.
ALTER TABLE merch_products
 ADD COLUMN fulfillment_organization_id uuid REFERENCES organizations(id) ON DELETE RESTRICT,
 ADD COLUMN online_enabled boolean NOT NULL DEFAULT true;
CREATE INDEX merch_products_fulfillment_org_idx ON merch_products(fulfillment_organization_id);
CREATE TABLE merch_product_events (
 product_id uuid NOT NULL REFERENCES merch_products(id) ON DELETE CASCADE,
 conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
 PRIMARY KEY(product_id,conference_id)
);
ALTER TABLE shop_order_items
 ADD COLUMN fulfillment_organization_id uuid REFERENCES organizations(id) ON DELETE RESTRICT,
 ADD COLUMN ship_from_snapshot jsonb NOT NULL DEFAULT '{}';
CREATE INDEX shop_items_fulfillment_org_idx ON shop_order_items(fulfillment_organization_id,order_id);
ALTER TABLE merch_inventory_events ADD COLUMN operation_key uuid;
CREATE UNIQUE INDEX merch_inventory_operation_idx ON merch_inventory_events(variant_id,operation_key) WHERE operation_key IS NOT NULL;
CREATE TABLE organization_shop_discounts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
 code citext NOT NULL UNIQUE,
 percent_off integer NOT NULL CHECK(percent_off BETWEEN 1 AND 99),
 active boolean NOT NULL DEFAULT true,
 starts_at timestamptz,
 ends_at timestamptz,
 max_uses integer NOT NULL DEFAULT 0 CHECK(max_uses>=0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(ends_at IS NULL OR starts_at IS NULL OR ends_at>starts_at)
);
CREATE TABLE organization_shop_discount_uses (
 discount_id uuid NOT NULL REFERENCES organization_shop_discounts(id) ON DELETE RESTRICT,
 order_id uuid NOT NULL UNIQUE REFERENCES shop_orders(id) ON DELETE CASCADE,
 amount_cents integer NOT NULL CHECK(amount_cents>0),
 PRIMARY KEY(discount_id,order_id)
);

-- Each warehouse needs its own tax calculation, recorded once after the common payment.
ALTER TABLE tax_quotes ADD COLUMN origin_key text NOT NULL DEFAULT '';
ALTER TABLE tax_transactions ADD COLUMN origin_key text NOT NULL DEFAULT '';
DROP INDEX tax_transactions_recorded_order_uq;
CREATE UNIQUE INDEX tax_transactions_recorded_order_origin_uq ON tax_transactions(order_id,provider,origin_key) WHERE status='recorded';
