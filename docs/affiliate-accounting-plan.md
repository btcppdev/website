# Ticket affiliate accounting repair

Status: implementation design following the September 14, 2026 code review.
No production accounting data has been changed.

## Intended behavior

A checkout shares 20% of its original ticket subtotal between the buyer and the
referrer. Affiliate codes permit buyer percentages 0, 5, 10, 15, and 20. A typed
code replaces a carried affiliate code. Tax, merchandise, shipping, and card
surcharges are outside this split.

Use integer minor units throughout ticket checkout. For currencies currently
represented in cents, a $65 ticket at 10% off costs 5,850 cents; buyer savings and
affiliate earnings are each 650 cents. Preserve a currency's actual minor-unit
scale when supporting currencies that do not have two decimal places.

Freeze the fiat-to-satoshi quote when the payment checkout is created, with source
and observation time. Store both fiat entitlements and computed satoshi amounts.
A delayed callback, retry, or later edit to the affiliate code must not change the
beneficiary or amounts. This is a quoted commission policy, not a claim to use the
historical market rate at the later settlement instant.

## Current failure points

- `RecordAffiliateUsage` inserts a new row without a checkout identity.
- Stripe's ticket callback repeats affiliate insertion and discount counters on
  replay. Its registration upserts do not protect other side effects.
- OpenNode tests for existing registrations before issuing tickets. That is not
  an atomic claim; concurrent callbacks can both pass. Later errors can cause a
  retry to skip credit because tickets already exist.
- `recordAffiliateUsageFromCheckout` reads the mutable discount and calls
  CoinGecko after payment. Missing data or FX errors only log and discard credit.
- Ticket discount calculations and form fields use whole currency units. A
  later multiplication by 100 cannot recover the fractions already truncated.
- Historical affiliate rows have no checkout reference or exchange-rate snapshot.

## 1. Immutable checkout quotes

Add a ticket checkout quote table. Persist before contacting a payment provider:

- Internal UUID and pricing version, conference/tier, currency and unit scale,
  quantity, original and discounted ticket amounts, surcharge, and full payable
  total, with merchandise/tax amounts separately identified.
- Stable discount and affiliate person IDs, plus code-name and affiliate-email
  snapshots. Nullable foreign keys must not erase the historical snapshots.
- Saved/earned fiat amounts, BTC rate as a fixed-precision decimal or rational,
  rate source and observation time, and saved/earned sats.
- Intended provider, expiry, and creation time. Reject unsupported currency or
  invalid quantities and monetary overflow before starting a provider checkout.

Use one quote for both conversions. Round the total 20% budget to sats once,
round the buyer share, and assign the remaining sats to the affiliate. The zero-
commission endpoint must always stay zero; independent rounding must not mint an
extra satoshi. Define minor-unit rounding explicitly and test odd-cent cases.

Persist the quote UUID in Stripe and OpenNode metadata. Store the returned provider
checkout ID in a separate binding with unique `(provider, checkout_id)` and a
unique quote binding. For providers with documented idempotency support, use the
quote ID as the creation idempotency key. Do not assume OpenNode supports the same
mechanism as Stripe. A callback arriving before the creation response is recorded
can recover the binding using the persisted quote and authoritative provider data;
reject a quote already bound to a different checkout.

If the rate cannot be obtained within the permitted freshness window, do not
create an affiliate checkout with an invented or absent commission. Preserve the
selected code and show a retryable checkout error. Do not silently drop attribution.
Ordinary checkouts with no affiliate need no affiliate FX quote.

## 2. Atomic, replay-safe paid settlement

Give both callbacks one shared settlement function, using a database transaction:

1. Authenticate the callback and confirm authoritative successful payment. Match
   provider, quote, currency, quantity, and payable amount. Keep provider-specific
   rounding tolerances explicit and bounded; do not trust browser fields.
2. Insert/lock a settlement keyed by `(provider, checkout_id)`, bound to the quote.
   A repeated identical settlement is a no-op; conflicting immutable facts fail
   for review. Do not use registration count as the accounting claim.
3. Write all ticket registrations through a transaction-aware `AddTicketsTx`.
4. Increment discount usage exactly once and insert affiliate usage with a unique
   settlement reference. Copy values from the quote rather than re-resolving the
   current discount or fetching a current exchange rate.
5. Mark settlement complete and commit. Any failure rolls back every accounting
   change and returns a retryable failure to the provider.

Keep receipts, newsletter subscriptions, and mixed-order post-processing outside
this accounting transaction. They must not gate whether commission is recorded.
Callbacks must still be able to resume unfinished post-processing when settlement
already exists. Any durable post-payment job should have its own unique key and
retry state; a single early return for an already-paid checkout is insufficient.

Use durable receipt/job recovery for acknowledged work and reconcile provider
settlements if callbacks exhaust provider retries. Store enough identifiers for a
repair command to replay the same settlement function safely. No repair path may
append an unkeyed affiliate usage row.

## 3. Cent-accurate ticket checkout

Keep current admin tier prices compatible, but convert into explicit minor-unit
values once on entry to checkout. Do not silently reinterpret existing `uint`
fields or discount expressions: `$10` still means ten major currency units.

Introduce explicit `...Minor`/`...Cents` values in the quote, form, and provider
adapters. Update:

- Percentage/fixed discount application, quantity totals, and surcharge rounding.
- The signed form payload, including pricing-version separation so stale forms
  cannot be read as prices in a different unit.
- `tix_details.tmpl`, `collect-email.tmpl`, and checkout JavaScript totals. Format
  decimal amounts at display time; do not multiply already-minor-unit values again.
- Stripe `UnitAmount`, OpenNode amount serialization, ticket subtotal metadata,
  and mixed-cart totals. Use decimal-safe serialization at the OpenNode boundary.

Revalidate current prices and eligibility when creating the immutable quote.
An existing provider checkout subsequently settles using its saved quote, not a
new tier price. An old browser form gets a refreshed quote before payment starts.

## 4. Migration and historical reconciliation

Add nullable settlement references to existing affiliate usages, with a partial
unique index for new referenced rows. Historical records remain unchanged.

For provider checkouts already open when this ships, separate a legacy settlement
path from new quote-based payments. Recover beneficiary and fiat pricing only from
authoritative metadata or independently verifiable records. Do not guess a missing
historical FX rate or beneficiary from a reused code name. Persist an unresolved
reconciliation item when evidence is insufficient; do not lose the ticket payment.

Before crediting a legacy checkout, determine whether an old unkeyed usage already
represents it. Without an unambiguous link, record it for review rather than risking
double payment. A dry-run reconciliation report should list candidate duplicates,
missing credits, unresolved matches, and proposed corrections. Apply approved
corrections as auditable adjustments, preserving original entries.

Refunds and disputes need a separate reversal policy, particularly for partial
refunds or already-paid commissions. The new schema should support uniquely keyed
adjustments; do not assume that deleting an original credit is sufficient.

## Validation and delivery

Logical implementation commits:

1. Minor-unit ticket pricing, forms, HMAC versioning, provider adapters, and tests.
2. Quote/settlement schema, snapshots, shared transactional fulfillment, and tests.
3. Callback integration and recovery, plus a dry-run historical reconciliation tool.

Required tests:

- $65 at 10% gives $58.50 paid and $6.50 earned; every affiliate slider stop,
  multiple tickets, odd cents, all supported currency scales, and zero commission.
- Full-price silent codes, explicit code overrides, deleted/renamed/reassigned
  codes after checkout creation, and stale/tampered browser forms.
- Stripe/OpenNode and mixed carts exclude tax, fees, and merch from commission.
- Sequential and concurrent duplicate callbacks create one credit/counter change.
- Failure after each transaction stage leaves no partial settlement; retry succeeds.
- FX outage prevents an unquoted affiliate checkout; FX changes after creation do
  not change a saved entitlement or affect webhook settlement.
- Notification/add-on failures do not lose or duplicate commission.
- Callback arrives before the provider binding is saved; mismatched or reused
  quote IDs cannot credit an unrelated checkout.
- Legacy rows stay intact; reconciliation is dry-run by default and refuses
  ambiguous matches. Re-running an approved correction is idempotent.
