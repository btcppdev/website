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
- Complete immediate prorated increases and next-renewal decreases. The read-only
  `PreviewIncrease` adapter now verifies the current paid period, requests an
  immediate-proration preview at a fixed timestamp, and validates the returned
  credit/debit lines, account/customer/subscription item, quantity, price, USD total
  and inclusive tax. It rejects unpaid time, pending changes, schedules, discounts,
  customer balance adjustments, unrelated lines and partial pages. Tests verify
  that previewing never mutates the subscription. This currently covers the
  two-line credit/replacement shape; real Stripe test-mode confirmation remains
  required before enabling it. Reviewed-quote persistence, pending-payment update
  submission, versioned quantity evidence, upgrade invoice fulfillment, renewal
  schedules and customer controls remain unfinished.

  The implementation follows Stripe's [invoice preview](https://docs.stripe.com/api/invoices/create_preview)
  guidance for reusing the proration timestamp. Confirmation will use
  [pending updates](https://docs.stripe.com/billing/subscriptions/pending-updates)
  so an unpaid increase cannot become an active quantity change.
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

The durable cancellation coordinator and account management page are described below.
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
The customer confirmation page and management link are described below.


## Account management page

`GET /services/alerts/manage` requires account sign-in and shows the most recent
account-owned card subscription after reading Stripe. The optional `account` query
binds the dashboard handoff to the same Bitcoin++ account. Redirect query flags
cannot claim cancellation. GET performs no provider mutation.

The confirmation form posts the checkout, reviewed revision/period, request UUID,
and CSRF token. POST reserves durable consent, attempts cancellation, and redirects
to the page. Pending and review outcomes remain visible and suppress another form;
provider errors do not discard the saved request. Paid access is never revoked by
this page. Accounts without a card subscription receive an explanation that Bitcoin
prepayments do not renew automatically.

Alerts exposes a Manage card subscription link when the card-checkout rollout flag
is enabled, using its configured Bitcoin++ origin and authenticated account ID.
Local HTTP/Stripe-SDK tests with PostgreSQL cover login continuation, CSRF, foreign
accounts, reviewed confirmation, duplicate POSTs producing one provider mutation,
and confirmed status. Svelte checks and build pass. Browser visual review of this
new central management page and live Stripe test-mode validation remain outstanding.

## Durable increase confirmation storage

Migration 116 stores immutable, account-owned increase quotes from the trusted
preview adapter. Saving and confirming recheck the reviewed subscription revision,
quantity, active state and billing period. Quotes expire within ten minutes or at
the period boundary. Confirmation reserves one pending instruction per checkout;
repeated confirmations recover its original outcome. Pending increases and
cancellations mutually exclude one another under the account billing lock.

PostgreSQL tests cover ownership, expiry, stale reviews, concurrent confirmations,
immutable quotes and resolved outcomes, conflicts in both directions, and no
entitlement grant from reservation. This storage is not exposed through HTTP yet:
provider coordination/recovery, paid proration fulfillment, quantity versioning and
the customer review UI remain required before enabling upgrades.

The `SubmitIncrease` provider adapter now rechecks the frozen preview at its
original second-precision proration timestamp and submits the existing item with
`always_invoice` and `pending_if_incomplete`. The durable quote ID supplies the
idempotency key. A changed review, unpaid base period or existing pending change
prevents submission. The result is only an invoice reference for reconciliation,
including whether Stripe left the change pending; it cannot grant access. Lost
responses propagate as errors for the coordinator to reconcile. Applied or pending
changes are not blindly resubmitted by this adapter.

Adapter tests cover request parameters, pending and applied responses, changed
review evidence, invalid provider responses and transport ambiguity. Real Stripe
test-mode validation, durable submission/recovery and paid invoice fulfillment
are still outstanding. Reference: [Stripe pending updates](https://docs.stripe.com/billing/subscriptions/pending-updates).

`PaidIncreaseInvoice` now re-reads the invoice and expanded price from Stripe and
verifies payment, ownership, the original quote's exact totals/tax, and both
proration lines. Its `PaidIncrease` evidence carries only the added slots from the
proration timestamp to the existing period end. It is deliberately distinct from
`PaidPeriod`; the monthly invoice verifier continues rejecting prorations.
Tests reject unpaid/partial payments, foreign ownership, changed quantities or
periods, wrong tax/price, credits, extra lines and incomplete pagination. This does
not itself fulfill an upgrade. The transaction below handles recording; provider
recovery and quantity-history integration remain needed before customer confirmation.

Migration 117 and `recordAlertsStripeIncreasePayment` add the transaction that
records verified upgrade evidence, a shop receipt, its payment event, the added
slot entitlement and an immutable updated quantity binding. It requires a saved
confirmation and a paid, unrevoked base-period grant. Repeated evidence returns
the same receipt; conflicting evidence rolls back the entire transaction. The
original monthly subscription/invoice evidence remains immutable. Prorated receipt
lines split rounding cents when necessary so their unit prices multiply correctly.

PostgreSQL tests exercise concurrent duplicates, missing confirmation/base payment,
late transaction failure, exact slot totals/expiry, receipt rounding and immutable
quantity evidence. This is still internal groundwork; the upgrade UI and submission
worker remain disabled until provider recovery is connected.

Migration 118 exposes the latest paid quantity binding to subscription refresh,
cancellation, new upgrade review, monthly invoice reconciliation and renewal-link
checks. Original checkout replay still uses the original immutable binding.
Upgrade fulfillment advances the reviewed-state revision. Previously recorded
monthly invoices retain their original quantity on callback replay. Recovery skips
already recorded upgrade invoices rather than misclassifying them as monthly bills.
Failed-renewal eligibility includes paid, unrevoked upgrade grants in the preceding
period. A PostgreSQL integration test verifies refresh after upgrading from two to
five paid slots, grace for five, renewal at five, and replay of the original
two-slot invoice without duplicate access.

`ReconcileAlertsStripeIncreaseInvoice` connects authenticated paid-invoice
verification to the atomic receipt/access transaction and requires the saved
confirmation. `RecoverAlertsStripeIncrease` can discover a lost invoice reference
through bounded paid-invoice pagination without making another charge attempt.
The existing reconciliation worker recovers pending upgrades before refreshing
subscription quantities; pending confirmations use a 15-minute retry lease and
reservation makes the checkout immediately eligible. PostgreSQL tests cover lost
reference recovery, unpaid and foreign-account rejection, and concurrent verified
callbacks returning one receipt. Submission orchestration, unpaid/authentication
flows, expiry handling and the customer review screen remain unfinished.

The `IncreasePaymentLink` adapter re-reads an existing invoice and price, checks
the exact saved quote and ownership, then returns only an HTTPS
`invoice.stripe.com/i/` URL for an unpaid open invoice. It neither charges nor
grants access. Paid, void, partially paid, mismatched and unsafe-link responses
are rejected. Adapter tests cover those cases and provider failures. The shared
reviewed-invoice validator keeps paid fulfillment and payment links aligned.
This adapter still needs the account-owned HTTP handoff and submission coordinator.
Stripe's [hosted invoice page](https://docs.stripe.com/invoicing/hosted-invoice-page)
provides payment and card-authentication UI; real test-mode validation remains.

Migration 119 persists upgrade submission attempts before provider I/O, with a
two-minute lease, attempt fencing and an immutable returned invoice reference.
`SubmitAlertsStripeIncrease` requires a saved confirmation and rechecks its billing
revision before claiming. Repeated requests reuse the same quote/idempotency key;
requests with an already saved invoice return that reference without another
provider call. A quote that expired without any attempt becomes expired; an
attempted operation stays pending until reconciliation resolves the ambiguity.
PostgreSQL tests cover concurrent claims, lease retry, cross-account rejection,
reference immutability, unattempted expiry and preservation of ambiguous attempts.
This coordinator is not yet invoked by HTTP or the worker: unpaid-reference
discovery, terminal provider-state handling and the customer UI remain required.

Unpaid reference recovery is now implemented: `LatestIncreaseInvoice` reads the
subscription and verifies its candidate invoice against the saved quote, while
`InspectIncreaseInvoice` rechecks a known reference. The account-owned recovery
getter persists that reference without another mutation at Stripe. Pending-upgrade
recovery leaves an open invoice unpaid and fulfills it only after paid verification.
`AlertsStripeIncreasePaymentLink` derives the invoice from this stored reference
and scopes the handoff to the quote owner. Adapter and PostgreSQL tests exercise
a lost response followed by open-invoice discovery, owner-only payment-link access,
no unpaid grant and eventual paid fulfillment. Terminal observations alone still
do not release the reservation; subscription-state confirmation and HTTP/UI wiring
remain unfinished.
