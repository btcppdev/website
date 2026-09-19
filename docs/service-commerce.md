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

## Shared Stripe invoice verification

`internal/servicestripe` now verifies paid full-period invoices against a trusted
product binding and the server plan registry. Alerts' existing invoice, renewal,
and failed-invoice terms checks delegate through an explicit Alerts binding.
Insider monthly and annual plans enforce one membership, exclusive tax, and the
matching recurring interval. The configured product tax code is mandatory; Insider
cannot inherit Alerts' SaaS tax code by default. Exclusive totals equal the fixed
line price plus verified nonnegative tax, with bounded arithmetic; inclusive Alerts
totals remain fixed. Refund/credit/proration and incomplete-line cases remain
ineligible for this full-period path.

Tax amount handling follows Stripe's invoice reference:
https://docs.stripe.com/api/invoices/object?api-version=2025-11-17.clover

Tests cover cross-product metadata, changed plans/quantities/intervals/tax codes,
extra charges, tax overflow, annual/monthly periods and zero tax. Existing Alerts
Stripe tests and targeted PostgreSQL lifecycle/settlement suites pass. This extracts
provider verification only: shared subscription storage, Insider subscription
checkout, invoice-to-order fulfillment and recurring cancellation/renewal wiring
remain required before Insider recurring billing is enabled.

## Shared subscription storage

The consolidated schema now stores checkout intents, subscription bindings and
invoice/order mappings in `service_stripe_checkouts`, `service_stripe_subscriptions`
and `service_stripe_invoices`. Checkout rows have an immutable product code and a
computed product family. Open checkout and request-key uniqueness are scoped to
account plus family, permitting concurrent Alerts and Insider purchases while
keeping monthly/annual Insider membership in one family. Provider session,
subscription and invoice identifiers remain globally unique.

The old `alerts_stripe_*` names for these three relations are filtered updatable
views over the shared tables, with check options; they are compatibility adapters,
not duplicate ledgers. Alerts workers and ownership checks still use those views.
The subscription insert that requires ON CONFLICT uses the canonical table after
loading the account-bound Alerts-only checkout. New shared/Insider coordinators
must bind and validate their expected product rather than use an Alerts view.
Account-merge manifests refer to the canonical checkout table and retain immutable
purchase/binding protections.

Fresh-schema migration and complete PostgreSQL getter/handler suites pass. Added
storage tests cover same-account/same-key coexistence, Insider plan-family
conflicts, immutable product identity, view visibility/mutation isolation and
provider subscription uniqueness. Shared storage is ready for the new coordinator;
Insider recurring checkout/renewal/cancellation remains unwired.

## Shared hosted subscription checkout

`internal/servicestripe.CheckoutParams` and `CheckoutClient.Open` now implement
subscription session creation/retrieval for server-owned product plans. Alerts'
existing adapter delegates to them while preserving its frozen return URLs and
`alerts-checkout:` idempotency keys. Insider plans generate quantity-one monthly
or annual sessions with exclusive tax, a configured classification, product-bound
metadata and return-to-Insider links. Product code, quantity, tax code and return
origin are validated even on saved-session lookup. The caller must authorize the
return origin for its product before freezing it; this is not a browser-controlled
allowlist. Provider responses must match account, checkout, product and live mode.

Tests cover stable retries, monthly/annual pricing and intervals, invalid return
origins, missing tax configuration, unknown products, extra membership quantity
and cross-product session responses. Existing Alerts checkout and targeted
PostgreSQL billing suites pass. Insider's public endpoint still uses fixed-term
payments until durable subscription reservation, binding and invoice fulfillment
are connected; this extraction alone does not enable recurring Insider charges.

Shared checkout reservations now persist Insider monthly/annual intent before
Stripe I/O, serialize against other account purchases, and reuse the frozen
verified email and checkout terms on retry. Monthly and annual membership share
one active reservation; Alerts has its own product family. Both directions of
membership prepaid/recurring overlap are guarded under the same account lock.
Saved session lookup requires the owning account and exact product; completed
sessions require reconciliation rather than a replacement charge. PostgreSQL
coverage exercises concurrent reservations, verified-email requirements,
account/product isolation, prepaid conflicts and provider session reuse.
These are internal primitives only: the public Insider checkout remains on its
existing fixed-term flow until recurring invoice fulfillment and lifecycle
handling are connected.

Subscription ownership binding and authenticated invoice lookup now use the
shared Stripe package. Alerts delegates through its existing adapter; Insider
can persist a product-scoped immutable binding after checking the completed
session, expanded recurring price, tax code, interval, account and quantity.
Repeated binding is idempotent and cannot replace the provider subscription.
Binding alone creates neither an entitlement nor an external fulfillment event.
The public Insider recurring route still awaits invoice-ledger and lifecycle
integration before activation.

Verified Insider recurring invoices now create shared shop orders, immutable
purchase/payment evidence, a product-scoped invoice record and a durable external
fulfillment job in one transaction. Invoice IDs deduplicate callbacks; overlapping
billing periods are rejected. Insider items remain pending until the receiver
acknowledges delivery and never create central Alerts entitlements. The common
Stripe payment writer retains Alerts' existing entitlement behavior.

Recurring purchase snapshots explicitly preserve the verified invoice period;
late settlement does not shift membership dates. Fixed-term purchases retain
settlement-relative access. PostgreSQL tests cover invoice replay, next-period
renewal, overlap rejection, fulfillment eligibility and queue deduplication.
Receiver tests exercise retries and both access-period policies. Public recurring
checkout is still disabled pending webhook/recovery and lifecycle integration.

The signature-verified Stripe callback now routes Insider recurring checkout
completion and paid invoices through product-scoped shared billing. Completion
only binds ownership; paid invoices are fetched again from Stripe before ledger
recording. Invoice-before-completion ordering can recover a saved session binding;
replays produce one invoice/order/fulfillment job. Fixed-term Insider checkout and
Alerts routing remain separate. Tests cover unsigned rejection, event ownership,
out-of-order delivery, duplicate delivery and deleted accounts. Pending external
fulfillment for a deleted account is ineligible and moves to review, preserving
financial evidence without granting access or retrying a NULL account forever.

Scheduled recovery, expiration and subscription lifecycle routing remain to be
connected before public recurring checkout is enabled.

Insider checkout expiration now uses shared authenticated provider verification.
Only expired, unpaid sessions with no subscription, invoice or payment intent
release a reservation; the database also rejects closure of a bound checkout.
Expired callbacks can recover a lost session reference and are idempotent. Shared
checkout recovery reads the saved session, binds a completed subscription and
reconciles its initial invoice without creating a charge. Integration tests cover
forged expiry state, repeated expiration, replacement reservation, bound-checkout
protection and missed completion/payment callbacks. Scheduled polling and renewal
lifecycle recovery still need wiring before public recurring checkout activation.

Subscription-state verification and paid-invoice discovery now live in the shared
Stripe package, with Alerts adapters retaining existing behavior. Verification
keeps failed renewal, voluntary cancellation, disputes, initial nonpayment and
paused collection distinct; observing state alone never grants access. Insider
payment recovery pages through paid invoices for the saved customer/subscription,
fully verifies unrecorded candidates, and preserves existing financial evidence.
Tests cover lifecycle distinctions and paginated renewal recovery with duplicate
replay. Scheduling and persisted Insider lifecycle state still need integration.

`SERVICE_STRIPE_RECONCILIATION_ENABLED=true` starts one shared recovery loop for
Alerts and Insider in the web process (default false). It requires a configured
Stripe test/live key and checks due work every minute. Each product receives an
independent 90-second pass budget and handles at most 20 saved checkouts; provider
attempts have a one-minute deadline and acquire a durable schedule lease before
I/O. A failure in one product does not prevent the other product's pass.

Alerts keeps its proration, scheduled decrease and cancellation recovery. Insider
uses saved checkout and paid-invoice recovery: recent unpaid sessions retry after
15 minutes, sessions with recorded payments hourly, older unconfirmed sessions
daily. Each product queue excludes closed/deleted accounts and missing provider
references. The queues deliberately select separate product families, while the
single startup worker dispatches both. Recovery never creates new checkout
charges. The former `ALERTS_STRIPE_RECONCILIATION_ENABLED` setting is superseded;
use the shared flag for both products.

PostgreSQL tests exercise simultaneous workers, durable leases, provider failures,
delayed retries and product/account exclusions. Dispatch tests verify independent
budgets and that one product's failure does not suppress the other. Insider
lifecycle persistence/management and public recurring checkout activation remain
outstanding.

Subscription observations now use `service_stripe_subscription_state`, with an
updatable Alerts-only compatibility view. Insider refreshes verify the saved
product/account binding and fetch Stripe under the account billing lock before
saving state. Identical observations preserve the revision; changed state advances
it. Failed verification leaves state unchanged. Observations do not rewrite paid
periods or grant grace. The updated single migration 128 was applied to fresh
`commerce_lifecycle_20260918`; prior scratch databases/checksums were unchanged.
Full PostgreSQL getter/handler suites and migration unit tests pass, including
Alerts compatibility, Insider visibility isolation and cancellation observations.
Lifecycle webhook/polling dispatch and management are still to be connected.

Insider lifecycle webhooks (subscription updated/deleted/paused/resumed and
invoice payment failure) now trigger authenticated subscription refresh after
loading the saved product/account binding. A stale webhook is only a lookup hint;
it cannot overwrite current provider state. Scheduled paid-invoice recovery also
refreshes lifecycle state after processing invoice pages, so a state verification
failure does not prevent already verified invoice payments from being recorded.
Integration tests cover stale cancellation events, paused payment failure and
unchanged invoice/fulfillment counts. Full PostgreSQL getter/handler suites pass.
Customer management, lifecycle delivery to Insider and public recurring checkout
activation remain outstanding.

Cancel-at-period-end provider mutation is now shared for Alerts and Insider.
It verifies account/product/price/interval and the reviewed period before changing
Stripe, refuses conflicting schedules or pending updates, and confirms returned
state. Repeated confirmed cancellation makes no second mutation; ambiguous errors
remain errors for the durable coordinator to retry. Alerts keeps its original
idempotency prefix; other service products use `service-cancel:`. Monthly/annual
Insider tests cover stale periods, conflicting changes, ambiguous provider errors
and unconfirmed responses alongside the existing Alerts cancellation suite.
The Insider account-owned cancellation coordinator and management API are not yet
connected; exposing cancellation still requires that durable consent boundary.

Cancellation consent now uses shared `service_stripe_cancellations` and one
product-scoped reservation function; Alerts retains a filtered compatibility
view and its quantity-aware processing adapter. Insider cancellation processing
requires a persisted account-owned request, serializes provider I/O, and preserves
ambiguous failures as pending. Recovery can confirm a provider mutation whose
response was lost without mutating twice. Pending requests wake scheduled recovery
and retry on its fifteen-minute cancellation schedule. Stale reviewed revisions,
conflicting requests and expired mutation windows cannot silently create a fresh
instruction. Confirmed cancellation only stops renewal; it does not refund or
rewrite paid access. Fresh `commerce_cancellation_20260918` migration and full
getter/handler suites pass; tests cover duplicate tabs, stale consent, lost provider
response recovery and Alerts/Insider isolation. The authenticated Insider management
API and cancellation confirmation delivery remain to be connected.

## Insider subscription management API

Authenticated Insider servers can POST JSON to:

- `/api/internal/insider/subscriptions/status`: `person_id`, `checkout_id`,
  `product_code` (Insider monthly or annual membership only). Refreshes verified
  Stripe state and returns `checkout_id`, `product_code`, `revision`, `observed_at`
  and `subscription`. The subscription snapshot includes `state`,
  `providerStatus`, `cancelAtPeriodEnd`, `collectionPaused`, `cancelAt`,
  `PeriodStart` and `PeriodEnd` (Unix seconds). Provider invoice IDs are omitted
  from the value exposed to the product. This describes billing, not content access.
- `/api/internal/insider/subscriptions/cancel`: the same identity fields plus
  UUID `request_id`, reviewed `revision` and `period_end`. Send matching
  `Idempotency-Key`. The account/product-scoped request is persisted before provider
  I/O. Returns `request_id` and `state`: `confirmed`, `pending`, or `review`.
  Pending work returns HTTP 202, including an ambiguous provider response; retry
  the same request ID, never invent a replacement because of a network error.

Both require the private Insider service bearer token and return no-store JSON.
Insider must authenticate its reader, derive `person_id` from that session, enforce
browser CSRF protection, and keep this service credential server-side. Missing
owned subscriptions return 404; stale reviewed state returns 409. Status refresh
failure returns 503 rather than treating stale data as current. These routes do
not create subscriptions, alter email consent, issue refunds or grant content.
Tests cover boundary rejection, actual PostgreSQL status/cancellation, cross-account
requests, and pending-to-confirmed replay after a lost provider response. Reader UI
wiring and cancellation confirmation delivery remain outstanding.

Status requests may alternatively send only `person_id`. Shared commerce resolves
the account's open, bound Insider subscription and returns its checkout/product
identifiers with the verified state. Partial identity pairs are rejected; explicit
checkout requests still require the matching product. Cancellation always requires
the explicit identity and reviewed revision/period returned by status. This lets
Insider keep its existing order references without duplicating the recurring
subscription mapping. Account discovery never returns Alerts subscriptions or
another reader's agreement; absence returns 404.

External fulfillment exposes aggregate Prometheus gauges for pending/accepted/
review jobs and oldest pending creation time. Labels contain only the bounded
state, never accounts, recipients or payment IDs. Protected collection uses the
existing business-metrics freshness/success signals. The fulfillment rules in
`ops/service-fulfillment-alerts.yml` warn on reviewed jobs and pending access
delivery older than fifteen minutes, with sustained-condition delays and fresh
collection required. PostgreSQL aggregate checks, collector privacy tests and
promtool rule tests pass. Deploying these rules and routing alerts to operators
remain launch tasks; no monitoring system was changed by this implementation.
