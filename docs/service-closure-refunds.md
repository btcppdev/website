# Permanent-closure prepaid refunds

Confirmed product rule: refund the unused proportion of the Bitcoin originally
paid, without converting the original USD price at the refund-date exchange rate.
This applies to permanent closure; ordinary cancellation does not issue a refund.

`servicebilling.EstimateBitcoinClosureRefund` computes the unused proportion for
one funded service period. It returns both floor and ceiling satoshi amounts,
plus the original amount and timestamps. It does not select a rounding policy,
create a refund record, change access, or contact a provider. The operator rounding
question is still pending. Timestamp precision is retained exactly so a later
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

## Outstanding execution requirements

- Capture verified original Bitcoin amounts at settlement. The current service
  OpenNode adapter verifies and records the original USD price, but its charge
  DTO does not retain the Bitcoin amount. Do not reconstruct it from current rates.
- Verify the provider's amount semantics, including overpayments, partial payments,
  provider fees and previous refunds. OpenNode documents separate overpayment
  fields in its [charge webhook guide](https://developers.opennode.com/docs/charges-webhooks);
  the authenticated [charge lookup](https://developers.opennode.com/reference/charge-info)
  must establish the refund basis. A callback alone is insufficient evidence.
- Resolve rounding, refund timing and payout mechanism; persist the policy and
  closure timestamp used for an executable refund plan.
- Reconcile previous refunds in the original asset before issuing more. This pure
  estimate must not be sent directly to a provider as a payout instruction.
- Provide a reviewable per-order plan, stop renewals, terminate access at closure,
  execute with durable identities, reconcile ambiguous responses and issue receipts.
- Rehearse provider-backed completion before treating closure refunds as shipped.
