-- Supplier payment instructions are private, encrypted, and versioned separately
-- from the shipping profile. Existing sales do not acquire new commercial terms.
CREATE TABLE organization_shop_payout_profiles (
 organization_id uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
 encrypted_details bytea NOT NULL,
 revision bigint NOT NULL DEFAULT 1,
 updated_by uuid REFERENCES people(id) ON DELETE SET NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE shop_order_items
 ADD COLUMN organization_fee_bps smallint,
 ADD COLUMN organization_fee_cents bigint,
 ADD COLUMN supplier_amount_cents bigint,
 ADD CONSTRAINT shop_item_supplier_split CHECK (
  (organization_fee_bps IS NULL AND organization_fee_cents IS NULL AND supplier_amount_cents IS NULL)
  OR (fulfillment_organization_id IS NOT NULL
   AND organization_fee_bps IS NOT NULL AND organization_fee_bps BETWEEN 0 AND 10000
   AND organization_fee_cents IS NOT NULL AND organization_fee_cents >= 0
   AND supplier_amount_cents IS NOT NULL AND supplier_amount_cents >= 0
   AND organization_fee_cents + supplier_amount_cents = unit_price_cents * quantity - discount_amount_cents)
 );

ALTER TABLE conference_pos_sale_items
 ADD COLUMN organization_fee_bps smallint,
 ADD COLUMN organization_fee_sats bigint,
 ADD COLUMN supplier_amount_sats bigint,
 ADD CONSTRAINT pos_item_supplier_split CHECK (
  (organization_fee_bps IS NULL AND organization_fee_sats IS NULL AND supplier_amount_sats IS NULL)
  OR (fulfillment_organization_id IS NOT NULL
   AND organization_fee_bps IS NOT NULL AND organization_fee_bps BETWEEN 0 AND 10000
   AND organization_fee_sats IS NOT NULL AND organization_fee_sats >= 0
   AND supplier_amount_sats IS NOT NULL AND supplier_amount_sats >= 0
   AND organization_fee_sats + supplier_amount_sats = price_sats * quantity)
 );
