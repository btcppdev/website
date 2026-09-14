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

1. Connect product pricing to account subscription state, tax allocation and expiring quotes.
2. Connect authenticated provider adapters to payment attempts and settlement records.
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

Migration 104 adds durable payment attempts and immutable settlement events.
`BeginServicePayment` checks account ownership and creates one attempt per
purchase before a provider call; that UUID is the provider idempotency key.
Subsequent calls retrieve the same attempt. `CanCreateCharge=false` means lookup
or reconciliation only, including an expired quote, canceled order, or existing
provider reference. Adapters must respect that flag and provider idempotency;
an ambiguous timeout must not create a new charge. Retry with a different order
is not a substitute for resolving the old payment attempt.

`BindServicePayment` stores an authenticated provider charge reference once,
with a unique provider/reference pair. `RecordServicePayment` accepts trusted
adapter output after successful provider verification, checks immutable quoted
amount/currency/provider and the bound reference, then atomically records the
event and settlement. It marks an unchanged pending order paid. Duplicate events
cannot overwrite a refund; reused event IDs with different content conflict.
Canceled or financially edited orders retain settlement evidence for reconciliation
without being marked paid. Product access is never granted here.

There are no public service payment handlers or live provider calls yet. Failed
attempt transitions, safe replacement after confirmed provider cancellation,
reconciliation queues, access fulfillment, BTC/satoshi settlement amounts and
receipt generation remain to implement. The event ledger currently records
verified USD settlement amounts, not a complete Bitcoin payment receipt.

Snapshots cannot be edited or deleted through ordinary SQL; account deletion
clears the account reference and retains purchase evidence. Existing order/item
columns remain mutable for legacy commerce: future settlement and receipt code
must use and verify the immutable snapshot rather than trust those mutable values.
Retention and anonymization of financial history still need final policy review.

## Alerts pricing

`internal/servicebilling` now calculates gross USD prices for monthly Stripe
slots and 12/24/120-month OpenNode terms, at 100 cents per paid slot per month.
Its slot input excludes the five free repositories. Amounts are checked against
the existing order schema's integer range before multiplication.

Calendar periods use UTC and clamp an anniversary to the last day of its target
month. Renewal scheduling must retain the original anchor: do not repeatedly add
months to a previously clamped February date.

An increase uses the fraction of elapsed time remaining in the original paid
period, with the original expiry unchanged. The implementation rounds half up
to a cent per slot before multiplying by added slots. Integer arithmetic avoids
floating-point drift and ten-year intermediate overflow. This rounding convention
is an implementation choice to expose in checkout terms before paid launch.
Zero-cent results stay zero; the current purchase transaction only accepts positive
orders, so zero-value adjustments and provider minimum charges need an explicit
lifecycle path before enabling increases. Decreases still require scheduling at
renewal in the subscription layer.

These calculations are not wired to a public checkout yet. Server-side tax
allocation, subscription revision checks, quote expiry, and provider reconciliation
remain necessary. Stripe integration must reconcile its charged proration with
the confirmed quote rather than assume its default calculation is identical.

Pricing tests cover all supported terms, leap years, month ends, half-cent
rounding, invalid periods and quantities, unchanged expiry and zero-price edges:

```sh
go test -race ./internal/servicebilling
```

Validation: migration 102 applied transactionally to the disposable PostgreSQL
database containing the combined Alerts/Insider migrations. Migration 103 and
purchase tests were also verified there, including eight concurrent retries,
changed quotes, account isolation, immutable evidence and injected rollback.
Run the tests with a disposable migrated test database:

```sh
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=... go test -race ./external/getters -run 'TestService(Payments|Purchase|OrderShape)Postgres' -count=1
```
