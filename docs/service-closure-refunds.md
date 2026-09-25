# Permanent-closure prepaid refunds

Confirmed product rule: refund the unused proportion of the Bitcoin originally
paid, without converting the original USD price at the refund-date exchange rate.
This applies to permanent closure; ordinary cancellation does not issue a refund.
Confirmed rounding: round down to the nearest whole satoshi, capped at the
original payment. Use `FloorSatoshis` from the calculator for this rule.
Confirmed payout flow: an admin reviews the amounts and pays from our wallet;
shared Bitcoin++ commerce records verified payout evidence. V1 does not initiate
OpenNode withdrawals. This recording flow must be shared with Insider.

`servicebilling.EstimateBitcoinClosureRefund` computes the unused proportion for
one funded service period. It returns both floor and ceiling satoshi amounts,
plus the original amount and timestamps. It does not select a rounding policy,
create a refund record, change access, or contact a provider. Rounding down is
the confirmed product rule. Timestamp precision is retained exactly so a later
whole-second policy can be applied explicitly rather than hidden in arithmetic.

The calculation uses actual elapsed time between the purchased start/end dates.
It caps the estimate at the original payment before/start of the period and returns
zero at/after expiry. Each separately paid upgrade must use its own funded window
and original payment. Never reuse the base period start for an upgrade refund.
No provider/network fee deduction is made by the calculator.

Tests cover 12/24/120 months, half-used and expired periods, leap-day boundaries,
mid-term upgrades, a fractional final satoshi, timezone equivalence, invalid inputs
and large-value/date overflow. The servicebilling race suite passed on September 25:
`/private/tmp/closure-refund-calculation-20260925.log`.

## Settlement evidence

The authenticated OpenNode `/v2/charge/{id}` lookup documents `price` in satoshis
and `fee` separately. The service adapter now freezes that price and any reported
fee, missing amount and overpayment in `service_payment_events.opennode_bitcoin`
within the settlement transaction. It preserves omitted optional fields as unknown.
USD order accounting remains separate. Paid observations with missing/invalid
Bitcoin prices or a nonzero missing amount cannot settle. Replays must match the
stored evidence; changes require reconciliation rather than overwriting it.
See the provider's [charge-info schema](https://developers.opennode.com/reference/charge-info).

This establishes the purchased charge price, not permission to pay out an amount.
Overpayments and prior refunds still require explicit review. Existing events
without Bitcoin evidence must not use a reconstructed exchange rate or silently
acquire a new refund basis. The existing immutable-event trigger protects this
new field as well.

Verification (September 25): the full fresh-database acceptance suite passed with
race detection and randomized order across `./cmd/... ./internal/... ./external/...`.
It used the current Alerts executable. The connected OpenNode test verifies missing
price rejection before settlement, concurrent settlement deduplication, persisted
price/fee/overpayment, conflicting replay rejection and database immutability.
Log: `/private/tmp/opennode-bitcoin-evidence-20260925.log`.
Database: `commerce_account_deletion_acceptance_20260925165525_46486`.
Provider calls used local fixtures; no real payment or refund was made.

## Outstanding execution requirements

- Rehearse captured Bitcoin evidence against a configured provider test charge,
  including overpayments, partial payments, provider fees and previous refunds.
  OpenNode documents separate overpayment
  fields in its [charge webhook guide](https://developers.opennode.com/docs/charges-webhooks);
  the authenticated [charge lookup](https://developers.opennode.com/reference/charge-info)
  must establish the refund basis. A callback alone is insufficient evidence.
- Resolve refund timing and implement verified recording of admin wallet payouts;
  persist the confirmed rounding rule and
  closure timestamp used for an executable refund plan.
- Verify the original-asset refund ledger and closure preview against provider
  refund evidence before issuing payouts. Preview output is not a payout instruction.
- Provide durable per-order approval/allocation, stop renewals, terminate access
  at closure, verify admin wallet payout evidence without duplicate allocation,
  reconcile ambiguous/manual payments and issue receipts.
- Rehearse provider-backed completion before treating closure refunds as shipped.

## Prior refunds and operator preview

`VerifiedServiceRefund.AmountSatoshis` records a verified Bitcoin refund alongside
its USD accounting amount. Unknown amounts remain null. The ledger serializes on
the order/payment lock, checks both asset balances, and rejects conflicting replays
or concurrent refunds that would exceed the original Bitcoin price. Missing or
conflicting original Bitcoin evidence, or an earlier refund with unknown satoshis,
prevents recording a further Bitcoin-denominated refund. No exchange rate fills
these gaps. The immutable refund trigger also protects the satoshi amount.

Inspect one prepaid order without sending a payment:

```sh
service-refunds -attempt PAYMENT_ATTEMPT_UUID -closure-at 2030-01-01T00:00:00Z
```

This reads the order's actual funded period and original charge price in a single
database snapshot. `remaining_refund_satoshis` uses the floor estimate minus earlier
verified refunds marked `permanent_closure`, never less than zero. Earlier goodwill
refunds need allocation review. Unknown Bitcoin amounts and absent/nonzero
overpayment evidence also produce review reasons and a null remaining amount.
Ordinary cancellation still does not issue refunds.

The preview neither reserves funds nor changes access, queues delivery, or calls
a payment provider. The manual payout workflow must revalidate and reserve under
the same order lock, freeze the reviewed closure time, and reconcile payout
evidence before recording completion. A transaction ID alone is not sufficient
to establish the paid amount and its allocation to an order.
USD/tax accounting for actual Bitcoin refunds—including amounts below one USD
cent—still needs implementation before payout execution; the current generic
refund recorder requires a positive USD accounting amount.

Verification (September 25): the complete fresh-database acceptance suite passed
with race detection and randomized order after these changes. Tests cover
concurrent Bitcoin balance exhaustion even when USD capacity remains, replay
identity, immutable satoshi amounts, unknown prior amounts, overpayment review,
floor rounding, previous closure refunds and expired periods. Log:
`/private/tmp/bitcoin-refund-planning-20260925.log`; database:
`commerce_account_deletion_acceptance_20260925170252_47672`.
The actual CLI was also run against its synthetic 1,001-satoshi payment at the
period midpoint: 500 satoshis unused, minus 200 already refunded, leaves 300.
No real payment, refund, access change or message was issued.

## Reuse with Insider

Alerts and Insider use the same central service orders, settlement evidence,
refund ledger and closure-preview function. A PostgreSQL race test runs the same
floor-rounding/prior-refund/expiry cases for `alerts.repository-slots` and
`insider.membership.annual`; both passed on September 25
(`/private/tmp/shared-insider-closure-20260925.log`). Product access handling and
receipt branding remain separate from these shared financial facts.

Insider already has reversal handling, access decisions and adjustment receipts.
Its current adjustment event and receipt model carry USD cents, but do not yet
carry the newly recorded refunded-satoshi amount. That field must be propagated
through the shared dispatch and Insider receipt storage/rendering. The separate
`codex/insider-commerce-converged` worktree is also behind this shared branch;
reconcile via rebase and connected tests before claiming deployed integration.
No separate Insider payment/refund ledger should be introduced.
