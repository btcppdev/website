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

Configure that Stripe webhook endpoint to deliver `checkout.session.completed`,
`checkout.session.expired` and `invoice.paid` for this flow, alongside the existing
merchandise events. The expiry event is documented in Stripe's
[event reference](https://docs.stripe.com/api/events/types#event_types-checkout.session.expired).

Migration 113 records verified subscription state. Also subscribe to
`customer.subscription.updated`, `.deleted`, `.paused` and `.resumed` (each with
the full `customer.subscription` prefix). These events and background polling
re-fetch the saved subscription, verify account/customer/price/quantity, and save
a revisioned state under the account billing lock. Duplicate observations do not
increment the revision. No subscription status changes paid-through dates.

The state distinguishes collection pauses, paused subscriptions, pending user
cancellation, completed cancellation, disputed payments, unpaid renewals and an
unpaid first purchase. Automatic cancellation with reason `payment_failed` remains
an unpaid-renewal candidate; it is not treated as a user-requested cancellation.
`renewal_unpaid` is not sufficient to grant grace. `FailedRenewalInvoice` now
re-fetches the invoice and Price and verifies the same account, quantity, full
monthly period and inclusive tax terms as a paid invoice. It additionally requires
an attempted automatic-collection renewal with an unpaid balance and an immediately
preceding paid period for the same subscription/quantity. Initial purchases,
unattempted invoices, voids, prorations and discontinuous periods are rejected.
Its due time is the original renewal period start and never moves with retries.
Migration 114 and the coordinator now load the preceding period from non-refunded,
non-revoked settlement records, verify the current subscription and persist a
revisioned renewal outcome. `invoice.payment_failed`, subscription callbacks and
background refreshes use this path. Repeated failure observations keep the same
revision and due time. Pause/cancellation/dispute states resolve pending failures;
verified payment resolves them atomically with the paid invoice/access grant.
Database triggers protect renewal identity and prevent resolved failures from
reopening. The account API's lifecycle projection and Alerts-side automatic grace
and reminder integration remain to build.
See [Stripe subscription states](https://docs.stripe.com/billing/subscriptions/overview)
and [collection pauses](https://docs.stripe.com/billing/subscriptions/pause-payment).

Remaining integration:

The account-owned `/services/alerts/checkout/{checkout}` return page now reads
verified invoice records and redirects to the shared receipt when available.
Without an invoice, it distinguishes pending confirmation, bound subscriptions,
closed checkouts and expired sessions. A CSRF-protected POST can resume the same
stored unpaid checkout; GET never creates a Stripe session or grants access.
Success/cancel query parameters are not payment evidence. The page returns users
to `https://alerts.btcpp.dev/alerts`. Handler presentation/template tests pass;
browser and Stripe test-mode end-to-end verification remain open.

- Implement failed-renewal grace, cancellation, pause and refund transitions.
- Implement immediate prorated increases and next-renewal decreases.
- Recover ambiguous creations without a session ID.
  The signed `checkout.session.expired` path now re-fetches the session and only
  closes a matching, expired, unpaid session with no invoice or subscription.
  Closure and subscription binding share account/row locks. Duplicate expiration
  events preserve the closed record and cannot affect a replacement checkout.
  Local age or a browser cancellation alone never releases the reservation.
  The return page now offers a CSRF-protected "Check with Stripe" action for
  saved sessions. It closes verified unpaid expirations or recovers a completed
  checkout's initial invoice through the existing binding and paid-invoice
  validators. It never creates provider sessions or charges. HTTP/PostgreSQL
  tests cover missing-expiry recovery, verified paid-invoice recovery and retries.
  Paid renewal invoices are now also recovered by the background worker below.
- Verify the new `/services/alerts/subscribe?account=...&slots=...` review/start
  route and Alerts handoff end to end. GET preserves the selection through login;
  POST requires the matching account and CSRF, reserves an immutable intent and
  opens the same Stripe session. Ambiguous creation errors return to the durable
  checkout page. Existing subscriptions still require the quantity-change flow.
  Alerts gates this handoff with `ALERTS_CARD_CHECKOUT_ENABLED` (default off),
  connects billing before checkout and activates oldest saved selections after
  a verified paid-access snapshot arrives.
- Exercise the full flow with Stripe test mode before enabling paid checkout.

Migration 112 adds a durable recovery schedule to checkout intents. Set
`ALERTS_STRIPE_RECONCILIATION_ENABLED=true` on Bitcoin++ to enable background
recovery with the configured Stripe key. Each minute the worker checks up to 20
known sessions/subscriptions within a three-minute batch deadline. Each checkout is
claimed separately using `FOR UPDATE SKIP LOCKED`, advancing its schedule before
provider I/O. A one-minute attempt deadline is shorter than the 15-minute retry
lease; unpaid attempts older than seven days retry daily. Subscriptions with
recorded invoices are checked hourly. Crashes leave a future retry time rather
than dropping the work. Closed sessions, deleted accounts and sessions without
provider references are skipped. For bound subscriptions, the worker lists paid
invoices scoped to the saved customer/subscription, paginating at 100 per page.
It skips recorded invoice IDs, and re-fetches each new invoice and its Price for
full payment/period/quantity/tax validation before granting access. Up to 100 pages
are allowed within the attempt deadline; partial progress is durable and retries
skip it. Local refunds remain untouched. Tests cover a missed renewal on a later
page, repeat polling, hourly scheduling and refund preservation. Proration and
subscription quantity changes still require their separate coordinator.

`TestAlertsStripeHTTPPostgres` exercises the start/return handlers with real
PostgreSQL records, cookie-backed account sessions and Stripe SDK HTTP calls to a
local mock provider. It verifies login continuation, account matching, CSRF,
server pricing, POST/query parameter isolation, repeat-submit session reuse,
changed-quantity conflicts and return-URL forgery without access grants. This
test passed against the isolated migrated database; it is not evidence of an
actual Stripe test-mode payment or deployed webhook delivery.

References: [Checkout Session creation](https://docs.stripe.com/api/checkout/sessions/create),
[subscription webhook events](https://docs.stripe.com/billing/subscriptions/webhooks),
[invoice listing and pagination](https://docs.stripe.com/api/invoices/list).

## End-of-period cancellation adapter

`Client.ScheduleCancellation` implements the provider operation for stopping future
renewals. The account coordinator must persist a `CancellationIntent` before calling
it and serialize billing changes. The intent binds a UUID idempotency key, creation
time, and reviewed period end. Requests older than 23 hours or past the reviewed
period require reconciliation instead of another mutation.

The adapter re-fetches the subscription, verifies its saved account/customer/price/
quantity, rejects changed periods, custom cancellation dates, managed schedules and
pending updates, and submits only `cancel_at_period_end=true`. An already verified
end-of-period cancellation returns without another mutation. The response must
still match the saved plan and original period. Transport errors remain ambiguous;
the caller must not report success or construct a replacement request. This method
does not revoke paid grants, refund payments, cancel immediately, or resume billing.

The durable cancellation coordinator is described below. The account confirmation
page and dashboard management link remain to implement; no customer route invokes
the adapter yet.
Provider-adapter tests and the existing PostgreSQL Alerts checkout/invoice tests
pass. No real Stripe cancellation has been performed.

Reference: [Stripe end-of-period cancellation](https://docs.stripe.com/billing/subscriptions/cancel).

Migration 115 now persists cancellation intents with immutable identity and
reviewed period. `ReserveAlertsStripeCancellation` checks account ownership and
subscription revision, commits consent before provider I/O, and reuses pending
instructions across duplicate submissions. `ProcessAlertsStripeCancellation`
serializes account billing changes, reads current provider state before retrying,
and atomically records the verified subscription/renewal state and cancellation
outcome. Verified paid grants are left intact. Ambiguous errors remain pending;
changed periods and requests past the safe retry window become `review`.

The existing opt-in reconciliation worker processes pending cancellations before
invoice recovery. Reservation makes the checkout immediately eligible; pending
cancellations use a 15-minute retry lease. A lost-response PostgreSQL test verifies
committed consent, recovery without a second mutation, duplicate submissions,
account isolation, immutable confirmed outcomes, and preserved paid-through dates.
The customer confirmation page and management link still remain to implement.
