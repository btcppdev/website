# Shared service commerce

Migration 102 extends existing `shop_orders` and `shop_order_items` for
`service` and `digital` purchases. It follows Alerts newsletter migration 101
and does not conflict with Insider migrations 096–100.

Service lines carry a server-selected product code and an explicit start/end
period. Digital lines carry a product code and exact resource identifier, with
no subscription period. Both exclude merchandise variants and conference
fulfillment. Service/digital line tax is included in the line total. Existing
physical item defaults remain valid.

This is a commerce foundation, not a working service checkout. `CreateShopOrder`
explicitly rejects service/digital orders and items, including mixed carts,
so callers must use the dedicated service purchase transaction. No new payment
endpoint or entitlement grant is exposed by this change.

Next work:

1. Product adapters that resolve server-owned prices, tax and access periods.
2. Provider attempts/events and verified settlement.
3. Subscription/prepaid records, product-specific access grants and reconciliation.
4. Receipts, refunds and migration of Insider membership fulfillment.
5. Alerts quantity changes and tax-inclusive proration.

Order item periods describe the purchased interval; they are not themselves an
access grant. Access must depend on verified settlement and product fulfillment.
Migration 103 adds immutable `service_purchase_requests` quote snapshots linked
to the account and order. `CreateServicePurchase` writes the pending order, item,
and snapshot atomically. Its trusted server quote is not a public request DTO:
product adapters must resolve prices and taxes before calling it. It validates
product/period shape, amounts, provider, expiration and verified account email.
Account-scoped keys serialize concurrent requests; an exact retry returns the
original order, and a changed quote conflicts. No provider calls or access grants
occur in this transaction.

Snapshots cannot be edited or deleted through ordinary SQL; account deletion
clears the account reference and retains purchase evidence. Existing order/item
columns remain mutable for legacy commerce: future settlement and receipt code
must use and verify the immutable snapshot rather than trust those mutable values.
Retention and anonymization of financial history still need final policy review.

Validation: migration 102 applied transactionally to the disposable PostgreSQL
database containing the combined Alerts/Insider migrations. Migration 103 and
purchase tests were also verified there, including eight concurrent retries,
changed quotes, account isolation, immutable evidence and injected rollback.
Run the tests with a disposable migrated test database:

```sh
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=... go test -race ./external/getters -run 'TestService(Purchase|OrderShape)Postgres' -count=1
```
