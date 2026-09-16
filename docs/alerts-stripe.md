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

Migration 120 records immutable void-resolution evidence. Recovery now releases
an attempted upgrade only after its submission lease has elapsed, a fresh invoice
lookup verifies `void`, and a fresh subscription lookup confirms the original
quantity with no pending update or schedule. It records that evidence and marks
the confirmation expired in one account-serialized transaction. An uncollectible
invoice, an active submission lease or an unresolved provider update remains
pending. Adapter and PostgreSQL tests cover those distinctions, repeated
resolution and immutable evidence. The customer review/status/payment pages and
automatic submission wiring remain outstanding.

`/services/alerts/upgrade/{quote}` now renders the account-owned saved review and
durable status. It displays the prorated charge, included tax, new paid-slot count,
period end and tax-inclusive monthly total. A CSRF-protected confirmation POST
reserves consent, tries read-only recovery and then submits using the durable
claim. Provider failures preserve the confirmation. A separate payment POST
resolves the saved invoice to Stripe; GET never submits a charge. Login continuation,
owner checks and a one-minute request deadline protect the handoff. HTTP tests
with PostgreSQL verify rendering, ignored forged success/amount query flags,
CSRF, foreign-account isolation and stale review rejection after cancellation.
The entry point that prepares a quote from the Alerts cart, full successful
HTTP confirmation/payment tests and browser visual review remain outstanding.

The existing `/services/alerts/subscribe` handoff now recognizes an account's
bound card subscription. Larger selections prepare a prorated upgrade review;
already covered selections go to billing management. New subscriptions retain
their existing checkout path. `PrepareAlertsStripeIncrease` refreshes provider
state, loads the current binding/revision, previews the change and saves the
quote with a revision recheck. Unchanged unexpired reviews are reused. A pending
confirmed upgrade always resumes its own review even if the cart changed.
HTTP/PostgreSQL tests verify handoff/reload reuse, covered selections and pending
confirmation preservation. Full successful HTTP payment and live provider tests,
repeat upgrades within one period, and browser visual review remain outstanding.

`TestAlertsStripeUpgradeHTTPPostgres` now exercises fresh quote creation through
the actual subscribe handoff, rendered review, two confirmation POSTs, an open
invoice requiring payment, the account-owned hosted-invoice redirect and eventual
paid fulfillment. It uses real account sessions, PostgreSQL and Stripe SDK HTTP
serialization against a local provider fixture. It verifies fixed proration timing,
payment-gated update parameters, one provider mutation despite duplicate POSTs,
ignored browser amount/quantity and success flags, no unpaid access and one
added-slot grant after repeated paid-page loads. Targeted Alerts Stripe tests pass.
This is not a real Stripe payment or browser visual test; those checks and repeat
upgrades within the same period remain outstanding.

Repeat upgrades within one period are now supported. Quote preparation selects
the recorded, unrevoked payment supporting the current quantity: either the
period's full monthly invoice or its latest paid upgrade. Its reference and prior
review are frozen with the new quote. The adapter freshly verifies that invoice
at preview and confirmation, including the paid quantity and period; nested
history is omitted. This also permits checking the recorded paid invoice when a
later void invoice occupies Stripe's latest-invoice field.

The HTTP fixture now completes two sequential upgrades (two → three → four paid
slots) before renewal and verifies that all grants retain the same expiry.
Adapter tests reject unpaid, credited, mismatched-period/quantity and wrong-ID
funding evidence. Stripe adapter and targeted PostgreSQL tests pass. Real Stripe
validation, browser review, unattended submission recovery and next-renewal
decreases remain outstanding.

`ProcessAlertsStripeIncrease` now unifies browser-confirmation processing with
the opt-in background reconciliation worker. It requires durable consent, tries
payment/reference recovery first, uses the fenced submission coordinator and
fulfills a synchronous paid result. Pending upgrade checkouts receive a two-minute
worker lease (longer than its one-minute attempt deadline) rather than waiting
past the quote's ten-minute validity window. The HTTP/PostgreSQL fixture also
simulates a crash immediately after confirmation: the actual worker submits once,
respects its lease, and later fulfills verified payment without a browser visit.
No real provider charge was made; production worker enablement and real test-mode
verification remain deployment checks.

Browser review of the production upgrade template/shared styles (local fixture,
without central navigation) covered review, pending payment and paid states at
320px, 390px and 1280px. Pricing now uses separated summary rows, secondary period
and tax copy, full-width wrapping actions and spaced footer links. The wordmark
uses Ubuntu Bold Italic with the shared joined-plus renderer. Computed layout
checks found no horizontal overflow at the mobile sizes. The upgrade HTTP tests
still pass after the markup changes. This does not verify real account navigation
or Stripe-hosted payment pages.

## Scheduled decreases in progress

The read-only `PreviewDecrease` adapter verifies account/subscription identity,
current quantity, period and absence of conflicting pending changes. It presents
the lower tax-inclusive monthly total effective at the existing renewal boundary.
Zero paid slots is represented as end-of-period cancellation.

The schedule parameter builder preserves the current quantity through that
boundary, then sets the lower quantity for a monthly phase and releases the
schedule afterward so the subscription continues at that quantity. Both the
update and phase transitions disable proration; automatic tax and the verified
price are retained. Tests cover these parameters, expired reviews, zero-slot
handling and conflicting subscription state. The builder is not yet connected
to the billing coordinator. Verified phase updates, renewal quantity transition
and customer controls remain required.

Stripe requires separate attach/update calls when scheduling changes on an
existing subscription: [subscription schedules](https://docs.stripe.com/billing/subscriptions/subscription-schedules),
[create schedule](https://docs.stripe.com/api/subscription_schedules/create).

Migration 121 persists immutable decrease quotes and account-owned confirmation
records. Saving/confirming rechecks the paid quantity, billing revision and renewal
boundary; duplicate confirmations recover the saved state. Pending, scheduled or
review-needed decreases exclude other decreases, upgrades and cancellations under
the account billing lock. Upgrade/cancellation reservations exclude decreases in
the opposite direction as well. Confirmation never changes current access.
PostgreSQL tests cover concurrency, ownership, stale/expired review, immutable
evidence, conflicts in both directions and preserved current quantity/expiry.
No decrease HTTP path or provider mutation is enabled in the coordinator yet.

The Stripe adapter can now attach a schedule using `from_subscription` alone,
with a stable key derived from the persisted confirmation ID. It checks current
ownership, quantity and reviewed billing period before calling Stripe, then
verifies the returned schedule/customer/subscription, release behavior and
unchanged current phase. A retry after a lost response repeats the same creation;
it never adopts an arbitrary existing schedule. Unit tests cover that retry,
expired consent and mismatched provider responses. These are mocked provider
tests, not live Stripe validation. The caller must persist the returned schedule
reference before updating phases.

Migration 122 and the attachment coordinator now persist a two-minute lease,
attempt counter and immutable schedule reference. Account ownership and the
reviewed billing revision/period are rechecked before claiming an attempt;
late responses are fenced by the attempt number. Saved references are reused
without provider creation. An expired review with no attempt can expire safely;
an ambiguous attempted attachment remains pending for recovery. PostgreSQL tests
cover concurrent claims, retry leases, foreign accounts, late responses and
reference immutability. Targeted Stripe getter/handler tests pass against the
local database.

Attachment recovery now accepts the immutable database first-attempt time as
evidence that consent was valid before review expiry. It repeats the identical
creation key/parameters for up to 23 hours, always before the reviewed renewal
boundary. Expired reviews without attempts expire; attempted requests beyond
the retry horizon move to review instead of allowing another creation. A routine
state refresh may advance the revision during recovery, but current ownership,
quantity, period and active status must still match. Adapter and PostgreSQL tests
cover recovery after review expiry and preservation of the first-attempt time.
The retry horizon stays below Stripe's documented 24-hour minimum idempotency
retention: https://docs.stripe.com/api/idempotent_requests.
The schedule update adapter now reads the saved schedule and current subscription
before writing. It preserves the current quantity until renewal, sets the lower
quantity afterward with no proration, and verifies both returned phases and
ownership metadata. Schedule defaults remain untouched; phase payment methods,
descriptions and metadata carry forward. Unsupported billing overrides are
rejected rather than silently discarded. An already-applied matching update is
recovered read-only, including after the write retry horizon. Adapter tests cover
phase boundaries, returned quantity mismatches, ownership, conflicting states,
preserved overrides and a lost update response. These tests use provider mocks.
Migration 123 and the schedule coordinator now persist phase-update leases and
immutable verified schedule snapshots. The schedule reference is constrained to
the saved attachment. Only a verified provider result can atomically mark the
decrease scheduled and advance the billing revision; duplicate completion is a
no-op and late responses from older attempts are rejected. Current binding and
paid grants remain unchanged. PostgreSQL tests cover ownership, lease/retry
behavior, invalid provider quantities, immutable evidence, idempotent completion,
single revision advancement and unchanged paid allowance. Targeted Stripe
adapter/getter/handler tests pass locally. Worker/UI wiring, operator resolution
of overdue ambiguous attempts and renewal-time quantity transitions remain
required before enabling decreases.
