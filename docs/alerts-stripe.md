# Alerts Stripe subscriptions

Implementation is in `internal/alertsstripe` and
`external/getters/alerts_stripe_checkout.go`. This is not yet a public, enabled
payment flow.

The initial Checkout Session uses subscription mode, one monthly recurring line,
$2 USD per additional repository, inclusive tax, and personal-use SaaS tax code
`txcd_10103000`. Stripe collects the billing address and calculates tax. The three
free repositories are excluded from the paid quantity. Card details remain on
Stripe's hosted checkout.

Migration 110 stores the account-owned checkout intent before contacting Stripe.
Its UUID is the provider idempotency key. Email, desired quantity, callback origin
and creation time are immutable. One unresolved checkout is allowed per account;
concurrent tabs requesting the same quantity reuse it. A changed quantity must
resolve the prior checkout first. Stripe and existing prepaid purchases share the
same account lock and cannot start overlapping plans.

A bound session is retrieved, never recreated. An unbound attempt can retry with
identical parameters for 90 minutes; its fixed Stripe session expiration is two
hours after reservation. Beyond that retry window, provider reconciliation is
required. Local timeout or age alone must never be treated as proof that Stripe
created no subscription. Completed sessions retain their reference for invoice
processing even though their checkout URL is no longer returned.

`PaidInvoice` retrieves the invoice and expanded Price/Product from Stripe. It
validates the persisted account/subscription/customer/price binding, mode,
quantity, recurring interval, inclusive tax, total paid and service period. Only
full initial and renewal invoices are handled by this verifier. Quantity-change
invoices require a separate proration transaction. Neither visiting the return
URL nor observing an active subscription grants access on its own.

Migrations 111 and the existing signed `/callback/stripe` endpoint connect
completed sessions to immutable subscription/customer/price bindings. A completion
event can recover a session reference after a lost creation response. `invoice.paid`
re-reads provider evidence and atomically records one shared order, line item,
purchase snapshot, payment attempt/event and access grant per invoice. Full-month
periods cannot overlap; exact replays preserve refund/revocation state. Stripe's
automatic invoice tax is recorded in the order and purchase snapshot without
creating a second Stripe Tax transaction.

Remaining integration:

- Implement failed-renewal grace, cancellation, pause and refund transitions.
- Implement immediate prorated increases and next-renewal decreases.
- Reconcile expired/abandoned sessions before releasing the account reservation.
- Expose authenticated checkout routes and connect the Alerts review screen,
  including resuming the originally selected repository/plugin after payment.
- Exercise the full flow with Stripe test mode before enabling paid checkout.

References: [Checkout Session creation](https://docs.stripe.com/api/checkout/sessions/create),
[subscription webhook events](https://docs.stripe.com/billing/subscriptions/webhooks).
