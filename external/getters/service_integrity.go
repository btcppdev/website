package getters

// SQL expression shared by settlement and reconciliation. Aliases o and r are
// the shop order and immutable service purchase request. Service checkout creates
// exactly one line; any additional, missing or changed line needs review.
const serviceItemsMatchQuoteSQL = `(
  (SELECT count(*) FROM shop_order_items i WHERE i.order_id=o.id)=1
  AND EXISTS (
    SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id
      AND i.quantity=(r.quote_snapshot->>'quantity')::bigint
      AND i.unit_price_cents=(r.quote_snapshot->>'unit_cents')::bigint
      AND i.line_total_cents=(r.quote_snapshot->>'total_cents')::bigint
      AND i.tax_amount_cents=(r.quote_snapshot->>'tax_cents')::bigint
      AND i.discount_amount_cents=0
      AND i.service_product_code=r.quote_snapshot->>'product_code'
      AND i.service_resource_id=r.quote_snapshot->>'resource_id'
      AND i.product_name_snapshot=r.quote_snapshot->>'name'
      AND i.service_period_start IS NOT DISTINCT FROM (r.quote_snapshot->>'period_start')::timestamptz
      AND i.service_period_end IS NOT DISTINCT FROM (r.quote_snapshot->>'period_end')::timestamptz
      AND i.fulfillment_method=CASE WHEN r.quote_snapshot->>'product_code'='insider.article' THEN 'digital' ELSE 'service' END
      AND o.checkout_kind=i.fulfillment_method
      AND i.product_id IS NULL AND i.variant_id IS NULL
      AND i.sale_conference_id IS NULL AND i.pickup_conference_id IS NULL
  )
)`
