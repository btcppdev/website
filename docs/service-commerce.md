# Shared service commerce

Migration 102 extends existing `shop_orders` and `shop_order_items` for
`service` and `digital` purchases. It follows Alerts newsletter migration 101
and does not conflict with Insider migrations 096–100.

Service lines carry a server-selected product code and an explicit start/end
period. Digital lines carry a product code and exact resource identifier, with
no subscription period. Both exclude merchandise variants and conference
fulfillment. Service/digital line tax is included in the line total. Existing
physical item defaults remain valid.

This is a schema foundation, not a working service checkout. `CreateShopOrder`
explicitly rejects service/digital orders and items, including mixed carts,
until a dedicated service purchase transaction is implemented. No new payment
endpoint or entitlement grant is exposed by this change.

Next work:

1. Server-owned product pricing, account-scoped idempotency and quote validation.
2. Immutable purchase snapshots, provider attempts/events and verified settlement.
3. Subscription/prepaid records, product-specific access grants and reconciliation.
4. Receipts, refunds and migration of Insider membership fulfillment.
5. Alerts quantity changes and tax-inclusive proration.

Order item periods describe the purchased interval; they are not themselves an
access grant. Access must depend on verified settlement and product fulfillment.
The new columns are not yet immutable; snapshot protection belongs in the service
purchase transaction and database lifecycle constraints before checkout launches.

Validation: migration 102 applied transactionally to the disposable PostgreSQL
database containing the combined Alerts/Insider migrations. Run the shape and
merchandise checkout gate tests with a migrated test database:

```sh
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=... go test ./external/getters -run TestServiceOrderShapePostgres -count=1
```
