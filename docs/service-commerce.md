# Shared service commerce

Migration note: the incremental, unpublished Alerts migrations described in the
implementation history below are consolidated into `128_alerts_service_billing.sql`.
Deploy this migration after the existing bitcoin++ master migrations. Development
databases containing the earlier migration sequence must be recreated separately;
do not rewrite their recorded migration checksums.

Migration 102 extends existing `shop_orders` and `shop_order_items` for
`service` and `digital` purchases. It follows Alerts newsletter migration 101
and does not conflict with Insider migrations 096–100.

## Product boundary and convergence status

This branch is based directly on the committed Alerts commerce implementation;
it does not replay Insider's earlier copies of migrations 102–105. Alerts'
migrations 106–108 and its OpenNode reconciliation, tax, account-revision, and
payment-time behavior remain the shared implementation. A blank database has
been migrated through the combined sequence and the focused Alerts and Insider
PostgreSQL commerce tests run together.

Shared commerce owns provider credentials, immutable quotes, orders, attempts,
settlement/refund evidence, tax facts, reconciliation, and canonical financial
receipt facts. Product adapters may choose server-side prices and validate their
own resource identifiers. The generic quote stores an opaque fulfillment
payload, and the generic external-fulfillment path dispatches verified settlement
without interpreting that payload or creating a second product ledger.

Insider remains authoritative for memberships, article access, policy grants,
open-access campaigns, subscriber consent, streams, cadence, and its customer
interface. Bitcoin++ stores only the versioned `none`/`free`/`paid` dashboard
projection and the delivery-provider records necessary to transport mail. The
renamed migration `096_insider_subscriber_projection` makes that limited role
explicit.

The old Insider-only refund draft is intentionally excluded. Recurring Stripe
invoice/subscription state, refunds, and provider-object mappings must be added
once here as generic commerce primitives and then dispatched to each product.
The account-merge manifest includes newsletter consent, service purchases, and
service entitlements. Their `person_id` fields are current account links that may
move through that audited merge workflow; immutable quote, order, product,
amount, period, settlement, and revocation evidence remains protected.

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
3. Subscription/prepaid state, product API integration and reconciliation workers.
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
Canceled or edited orders retain settlement evidence for reconciliation
without being marked paid. Product access is never granted here.

Settlement also requires exactly one order item matching the immutable quote's
product, resource, quantity, price, tax, name, fulfillment method and access period.
Existing item rows are locked during validation. Missing or altered lines appear
as `order_item_mismatch` in reconciliation; the paid financial event remains
recorded. Access fulfillment revalidates the immutable snapshot because legacy
order items remain editable after settlement.

Migration 105 adds durable service access grants. `FulfillServicePurchase` locks
the order and items, requires verified settlement and an unchanged paid purchase,
then creates one grant per purchase and marks its item fulfilled atomically.
Unpaid, canceled, refunded, expired, altered or partially processed purchases need
review. Retrying fulfillment returns the original grant without reactivating it.
Grant fields cannot be rewritten; revocation requires a recorded actor and reason.

`ActiveServiceEntitlements` returns only the requesting account's grants within
their purchased periods, with an exclusive end boundary. Revoked grants and orders
in refund states provide no access. Partial refunds currently withhold access
pending product-specific quantity reconciliation; that lifecycle must be finished
before partial refunds are exposed. Account removal clears the grant's account
reference but preserves financial evidence.

No worker invokes fulfillment yet and neither
Alerts' allowance cache nor Insider's OAuth membership/article claims consume the
new grants. Subscription renewals, quantity changes and provider switching still
need the product-level state machine. Tests cover concurrent fulfillment, period
boundaries, account isolation, refunded purchases and replay after revocation.

## Account access API

`GET /api/v1/me/entitlements` requires `entitlements:self:read` on an OAuth access
token or personal access token. The explicit consent label covers purchased service
and article access. Identity/profile/newsletter scopes alone do not grant access.
The response uses the standard API envelope:

```json
{"data":{"accountId":"account-uuid","asOf":"2026-09-14T12:00:00Z","entitlements":[]},"meta":{"request_id":"..."}}
```

Each grant includes `id`, `productCode`, `resourceId`, `quantity`, `startsAt` and
`endsAt`. Article grants have null time boundaries and an exact publication ID.
Only active grants are returned; an empty array means no current purchased access.
This is not a subscription history, upcoming-renewal schedule or total repository
allowance: Alerts must add its free allowance and apply product billing state.

`accountId` is the stable token-owner ID; consumers must match it to the local
signed-in account before accepting access. The account and evaluation time come
from authentication and the server clock;
query parameters cannot override either. Responses are private/no-store. Consumers
must respect end times and refresh for refunds/revocations; they must not treat a
successful old response as indefinite access. OAuth clients need the new scope
registered and consented before calling this route. Product integration and live
OAuth verification remain to implement.

There are no public service payment handlers or live provider calls yet. Failed
attempt transitions, safe replacement after confirmed provider cancellation,
reconciliation actions, access fulfillment, BTC/satoshi settlement amounts and
receipt generation remain to implement. The event ledger currently records
verified USD settlement amounts, not a complete Bitcoin payment receipt.

`ReviewServicePayments` supplies a read-only, cursor-paginated operator/worker
report (100 rows per page). It identifies payments received after cancellation,
settled orders with changed financial totals, settlement not applied to an order,
paid purchases whose account was removed, paid orders awaiting item fulfillment,
and attempts unresolved after 15 minutes. References and amounts are included;
buyer emails/names and raw provider payloads are not. A report entry authorizes no
money movement or access grant. Global admins can view the report at
`/admin/service-payments`, linked from the Orders page. The route uses the existing
global-admin authorization, private/no-store responses, validated UUID cursors,
and escaped HTML rendering. No mutation actions are offered. There is no provider
lookup/reconciliation worker consuming it yet. Authentication redirect and template
escaping tests pass; browser layout and a real authenticated admin session still
need verification before deployment.

Fully refunded orders do not re-enter paid-order fulfillment checks. Unsettled
attempts still need investigation even if the local order was canceled: local
cancellation does not prove that a provider charge was canceled. Scan all pages
and then start a fresh scan to find rows whose state changed behind the cursor.

Snapshots cannot be edited or deleted through ordinary SQL; account deletion
clears the account reference and retains purchase evidence. Existing order/item
columns remain mutable for legacy commerce: future settlement and receipt code
must use and verify the immutable snapshot rather than trust those mutable values.
Retention and anonymization of financial history still need final policy review.

## Alerts pricing

`internal/servicebilling` now calculates gross USD prices for monthly Stripe
slots and 12/24/120-month OpenNode terms, at 200 cents per paid slot per month.
Its slot input excludes the three free repositories. Amounts are checked against
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
Alerts Bitcoin checkout now starts at `/services/alerts/prepay?account=...&slots=...&months=...`.
GET preserves the account and 12/24/120-month choice through login, verifies account
ownership and shows the billing-address form. Its CSRF-protected POST derives
tax-inclusive pricing from central billing state and stores a quote for the existing
review/confirm/order/OpenNode flow. Browser prices, tax amounts and query-string
overrides are ignored. Preparing a quote does not start payment or grant access.
Both OpenNode and Stripe tax credentials are required. Real-session PostgreSQL
tests with a local Stripe tax fixture verify ownership, CSRF, inclusive SaaS tax,
the stored term, and absence of orders at quote preparation. The same HTTP test
now continues through quote review, repeated order confirmation, repeated hosted
checkout handoff, signed callback, authenticated unpaid/paid lookup, tax transaction
recording, account-access projection and the paid receipt. Forged callback payment
fields cannot grant access; retries create one charge, payment event and grant,
with exactly the purchased quantity and term. This uses local provider fixtures,
not real OpenNode settlement. The prepaid entry template and production styles
were visually checked using fixture data at 390px and 1280px. The review fixed
inline address labels/inputs by applying the shared form styles and responsive
columns. Live settlement and a browser pass with real account sessions remain
outstanding.

Run the tests with a disposable migrated test database:

```sh
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=... go test -race ./external/getters -run 'TestService(Payments|Purchase|OrderShape)Postgres' -count=1
```

## Combined Alerts/Insider checkpoint

The current linear integration branch incorporates the additive Insider convergence
work from `a1669b33` over the current Alerts implementation. Projection/campaign
tables are included in the single unpublished migration 128. Existing development
databases need a fresh combined schema; no applied migration history is rewritten.
Shared purchase validation accepts Insider monthly/annual, article and open-access
products. Insider acknowledges fulfillment without a central content-access grant;
central grants remain Alerts-only. External fulfillment rejects Alerts purchases.
The Alerts receipt worker retires Insider jobs without sending, leaving Insider's
receipt presentation/delivery path responsible for them.

Fresh combined migration application and the complete PostgreSQL getter/handler
suites pass. This is not a complete shared subscription implementation: the Stripe
lifecycle is still Alerts-specific, Insider fixed-term checkout does not establish
a recurring agreement, and durable product dispatch/recovery still needs work.
Insider's proposal to relax immutable purchase/account bindings was not imported;
account merges require a coordinated design covering all provider/quote bindings.

Migration atomicity follow-up: removed transaction wrappers inherited from the
three Insider migration sources. The consolidated file uses only the runner's
transaction. A scratch-database exercise appended a deliberate exception after
all statements and verified that service tables, Insider projection tables and
the migration record were absent after rollback. Applying the unmodified file
then succeeded; repeat startup applied zero migrations. The unit suite rejects
standalone transaction-control statements in migration 128. Earlier local test
databases retain their original checksums; no deployed database was modified.

## Durable external fulfillment

Verified Insider settlement now creates `service_fulfillment_jobs` in the same
transaction as the payment evidence. The primary key is the payment attempt, so
provider retries cannot create a second fulfillment job. Initial callbacks try
immediate dispatch; `SERVICE_FULFILLMENT_ENABLED=true` starts the background worker
when the Insider endpoint, token and merchant name are configured. No callback
retry is required to recover a queued job after process restart or receiver outage.

Jobs have one-minute leases and monotonic attempt numbers. The worker rechecks
paid order integrity before sending, freezes its first rendered JSON payload and
uses a stable event ID. Failures back off from one minute to one hour. Accepted
jobs are terminal; late worker responses cannot change a newer attempt. A lost
acknowledgement may repeat the same event, so the receiver must remain idempotent.
Product acknowledgement precedes marking the service item fulfilled; a crash in
between retries the same event. HTTP redirects are refused. Invalid/cancelled
orders move to review rather than granting access.

PostgreSQL tests cover an unavailable receiver, byte-identical retry after merchant
configuration changes, lease exclusion/recovery, late responses, repeated provider
callbacks, cross-product grants and receipt separation. Full getter/handler suites
pass against the fresh combined schema. Monitoring, reviewed operator recovery,
account deletion/redaction and refund/revocation dispatch remain to complete. This
queue currently dispatches verified settlement only, not the complete subscription
lifecycle, and has not been enabled against a live product endpoint.

## Shared server-owned plans

`internal/servicebilling/products.go` now resolves Alerts repository slots and
Insider monthly/annual membership plans. Alerts Stripe checkout and Insider quote
and settlement-date construction use this registry. Membership quantity is one;
Alerts permits paid slot quantities. The registry distinguishes Alerts inclusive
tax from Insider exclusive tax, and separates their subscription families so an
Insider interval change will not be mistaken for a separate Alerts subscription.
The family is currently policy metadata; database subscription uniqueness and
lifecycle coordinators still need to consume it.

Membership boundaries use the shared calendar-clamping rule, including January 31
and annual February 29 starts. Insider monthly OpenNode requests are rejected;
annual prepaid remains supported. Alerts prepaid continues through its existing
12/24/120-month pricing adapter. Insider's tax classification and provider tax
calculation still require integration; the registry alone does not collect tax or
make the existing fixed-term Insider checkout recurring. Tests cover prices,
quantity separation, unsupported plans/providers and calendar boundaries.
