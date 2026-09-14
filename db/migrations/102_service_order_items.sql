-- Shared commerce representation only. Provider settlement and product-specific
-- entitlement fulfillment must be connected before exposing service checkout.
ALTER TABLE shop_orders DROP CONSTRAINT shop_orders_checkout_kind_check;
ALTER TABLE shop_orders ADD CONSTRAINT shop_orders_checkout_kind_check
  CHECK (checkout_kind IN ('merch', 'ticket', 'mixed', 'service', 'digital'));

ALTER TABLE shop_order_items DROP CONSTRAINT shop_order_items_fulfillment_method_check;
ALTER TABLE shop_order_items ADD CONSTRAINT shop_order_items_fulfillment_method_check
  CHECK (fulfillment_method IN ('ship', 'event_pickup', 'pos_takeaway', 'service', 'digital'));

ALTER TABLE shop_order_items
  ADD COLUMN service_product_code text NOT NULL DEFAULT '',
  ADD COLUMN service_resource_id text NOT NULL DEFAULT '',
  ADD COLUMN service_period_start timestamptz,
  ADD COLUMN service_period_end timestamptz,
  ADD CONSTRAINT shop_order_items_service_shape CHECK (
    CASE WHEN fulfillment_method IN ('service', 'digital') THEN
      btrim(service_product_code) <> ''
      AND product_id IS NULL AND variant_id IS NULL
      AND sale_conference_id IS NULL AND pickup_conference_id IS NULL
      AND tax_amount_cents <= line_total_cents
      AND CASE WHEN fulfillment_method = 'service' THEN
        service_period_start IS NOT NULL AND service_period_end IS NOT NULL
        AND service_period_end > service_period_start
      ELSE
        btrim(service_resource_id) <> ''
        AND service_period_start IS NULL AND service_period_end IS NULL
      END
    ELSE
      service_product_code = '' AND service_resource_id = ''
      AND service_period_start IS NULL AND service_period_end IS NULL
    END
  );

COMMENT ON COLUMN shop_order_items.service_product_code IS
  'Server-selected product, e.g. alerts.repository-slots, insider.membership, insider.article.';
COMMENT ON COLUMN shop_order_items.service_resource_id IS
  'Product-scoped resource identifier; required for one-time digital access.';
COMMENT ON COLUMN shop_order_items.tax_amount_cents IS
  'For service/digital items tax is included within line_total_cents, not added to it.';
