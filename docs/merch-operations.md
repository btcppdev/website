# Merch shop operations

## Required production configuration

The web process refuses to start in production unless its payment, webhook,
mailer, and carrier credentials are present. Configure these in the deployment
platform's encrypted environment settings; do not commit their values.

```text
STRIPE_KEY
STRIPE_END_SECRET
OPENNODE_KEY
OPENNODE_ENDPOINT
EASYSHIP_API_KEY
EASYSHIP_ENDPOINT=https://public-api.easyship.com
EASYSHIP_API_VERSION=2024-09
EASYSHIP_WEBHOOK_SECRET
```

Set the Easyship origin to the actual fulfillment location at
`/admin/easyship`. Checkout fails closed in production when the database origin
is missing or Easyship cannot quote a shipped order. Local development retains
the flat-rate fallback so the UI can be exercised without live carrier
credentials.

Register `https://btcpp.dev/callbacks/easyship` for production and
`${LOCAL_EXTERNAL}/callbacks/easyship` for sandbox testing. Use separate
`webh_...` secrets in each environment. The callback stores signed events in a
replay-safe inbox and the shop maintenance worker applies them asynchronously.

Stripe Tax must be enabled for the Stripe account. Card payments use automatic
tax in Checkout. Bitcoin shipped orders create a Stripe Tax calculation before
the OpenNode charge and convert it to a tax transaction after payment.

## Checkout and inventory lifecycle

- Checkout sessions expire after 30 minutes.
- Inventory reservations have a five-minute provider-webhook buffer.
- The web process scans once per minute and cancels expired pending orders,
  releases their reservations, and restores stock.
- Payment webhooks and refund writes are idempotent. Replayed provider events
  must not duplicate sales, refunds, emails, or inventory events.
- A paid order cannot be cancelled; use the refund workflow instead.

The merch admin page shows pending/overdue orders, stale reservations, paid
orders missing a provider reference, and variants at five units or fewer.

## Reconciliation queries

Run these against a read-only production connection while investigating:

```sql
SELECT status, count(*) FROM shop_orders GROUP BY status ORDER BY status;

SELECT id, public_id, created_at, checkout_expires_at
FROM shop_orders
WHERE status = 'pending' AND checkout_expires_at <= now()
ORDER BY checkout_expires_at;

SELECT r.id, r.checkout_session_id, r.quantity, r.expires_at, o.public_id
FROM merch_inventory_reservations r
JOIN shop_orders o ON o.id = r.checkout_session_id
WHERE r.status = 'active' AND r.expires_at <= now()
ORDER BY r.expires_at;

SELECT o.public_id, o.payment_provider, o.payment_provider_id, o.paid_at
FROM shop_orders o
WHERE o.status IN ('paid', 'partially_refunded', 'refunded')
  AND o.payment_provider_id = '';

SELECT v.sku, v.label, coalesce(sum(e.quantity_delta), 0) AS stock
FROM merch_variants v
LEFT JOIN merch_inventory_events e ON e.variant_id = v.id
GROUP BY v.id, v.sku, v.label
ORDER BY stock, v.sku;
```

## Deployment smoke test

1. Apply migrations before accepting traffic (the web startup also applies
   them, but an explicit migration job makes failures easier to see).
2. Confirm the merch admin Operations panel contains no overdue checkout or
   reservation counts.
3. Place one Stripe test order and replay its webhook; confirm one receipt and
   one sale inventory event.
4. Place one OpenNode test order and confirm its tax quote and tax transaction.
5. Start and cancel a checkout; confirm the stock count returns to its original
   value.
6. Quote at least one domestic and one international shipping address.

For a paid shipped order, open its admin detail page and first select
`Create Easyship shipment`. After creation succeeds, select `Purchase Easyship
label`; this second action books the courier and spends Easyship balance, so it
requires explicit confirmation. Both calls use stable idempotency keys. The
generated label and tracking link appear on the order. The manual shipment form
remains available for labels purchased outside Easyship.
