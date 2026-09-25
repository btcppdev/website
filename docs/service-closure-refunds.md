# Permanent-closure prepaid refunds

Confirmed product rule: refund the unused proportion of the Bitcoin originally
paid, without converting the original USD price at the refund-date exchange rate.
This applies to permanent closure; ordinary cancellation does not issue a refund.
Confirmed rounding: round down to the nearest whole satoshi, capped at the
original payment. Use `FloorSatoshis` from the calculator for this rule.

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
- Resolve refund timing and payout mechanism; persist the confirmed rounding rule and
  closure timestamp used for an executable refund plan.
- Reconcile previous refunds in the original asset before issuing more. This pure
  estimate must not be sent directly to a provider as a payout instruction.
- Provide a reviewable per-order plan, stop renewals, terminate access at closure,
  execute with durable identities, reconcile ambiguous responses and issue receipts.
- Rehearse provider-backed completion before treating closure refunds as shipped.
