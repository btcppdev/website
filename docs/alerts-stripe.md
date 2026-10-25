# Alerts Stripe subscriptions

Migration note: the incremental, unpublished Alerts migrations described in the
implementation history below are consolidated into `128_alerts_service_billing.sql`.
Deploy this migration after the existing bitcoin++ master migrations. Development
databases containing the earlier migration sequence must be recreated separately;
do not rewrite their recorded migration checksums.

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
`SERVICE_STRIPE_RECONCILIATION_ENABLED=true` on Bitcoin++ to enable background
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

The read-only renewal transition adapter now checks that the saved schedule has
entered its lower-quantity phase, or has completed/released after reaching that
phase, and that the current subscription has the expected lower quantity and
renewed period. It rejects early transitions, foreign references and mismatched
quantities. Active and unpaid renewals both produce an expected billing binding;
neither grants access. Paid invoices and eligible failure grace still require
their separate verification. Adapter tests cover active/unpaid transitions,
completed/released schedules and invalid observations.

Migration 124 adds immutable decrease transition evidence and a shared ordered
quantity history for upgrades and decreases. Existing upgrade evidence is
backfilled; triggers append subsequent verified changes in the same transaction.
Current subscription readers (including upgrade fulfillment) use this history,
while original checkout bindings and invoice facts remain immutable. A due
scheduled decrease is reconciled before subscription refresh validates the
current quantity. Persisting its observed transition marks it applied and updates
the billing snapshot, without issuing grants. PostgreSQL tests verify lower
current quantity, preserved original binding/invoice quantity and idempotent
history insertion; targeted Stripe getter/handler suites pass.

Failed renewal projection now carries only the newly selected quantity into grace
when an immutable applied decrease matches the exact renewal boundary, old paid
quantity and current binding. The prior invoice remains unchanged, and arbitrary
quantity mismatches remain ineligible. The same failed-invoice verifier then
checks ownership, price, attempted payment and monthly terms. PostgreSQL tests
exercise a three-to-one decrease with a one-slot failed invoice, repeat recovery
without moving its due date, and rejection of unmatched or unapplied decreases.
The existing 14-day grace/reminder policy uses that verified one-slot renewal
fact. Full renewal HTTP/provider fixtures and customer controls remain required
before enabling decreases.

The decrease review preparer now verifies account ownership before provider reads,
refreshes the current binding/revision, and saves the reviewed lower quantity,
tax-inclusive monthly total and renewal date. Reloads reuse an unexpired unchanged
review; an existing pending/scheduled/review-needed instruction resumes its
original quote instead of silently replacing consent. Zero paid slots is reserved
for cancellation. PostgreSQL tests cover foreign accounts, invalid quantities,
review reuse, no implicit confirmation and resuming saved consent. Customer-facing
review selection from the billing page still needs wiring.

`GET/POST /services/alerts/decrease/{quote}` now renders an account-owned saved
review with its lower quantity, tax-inclusive monthly total and UTC effective
date. GET only displays persisted state. CSRF-protected confirmation saves consent
and starts the shared decrease coordinator; transport failures leave a resumable
pending request. Status copy distinguishes pending, scheduled, applied, expired
and review-needed outcomes. The screen reuses the compact upgrade layout.
Local HTTP/PostgreSQL tests cover rendering, forged query values, no GET-side
confirmation, foreign ownership, CSRF and stale review rejection. Visual browser
verification and the complete successful provider HTTP flow remain pending.

Billing management now shows the current paid quantity and a lower-quantity
selection for eligible active subscriptions. The CSRF-protected selection POST
prepares a review and redirects to its saved terms; it does not confirm the
change. Zero paid slots directs users to cancellation. Pending/scheduled decreases
show a link back to their status and suppress conflicting controls. The page uses
the existing compact billing styling and lowercase bitcoin++ wordmark. HTTP
tests cover review handoff/reuse, zero or unchanged quantities and CSRF rejection.
Withdrawing a saved schedule and canceling while a decrease is scheduled still
need implementation; browser verification and full successful provider lifecycle
fixtures also remain outstanding.

The schedule withdrawal adapter now releases an owned, verified decrease schedule
before renewal, preserving the current subscription quantity and cancellation
date. It checks the current subscription before release and re-reads it afterward;
a lost response recovers an already released schedule without another write.
Writes stop within 30 seconds of the renewal boundary and after the 23-hour
request retry horizon. Adapter tests cover retained quantity, response loss,
foreign/mismatched references, changed quantity and unsafe timing. Durable
withdrawal consent/evidence, worker processing and customer controls are not yet
wired. Stripe release semantics: https://docs.stripe.com/api/subscription_schedules/release.

Migration 125 now stores account-owned withdrawal consent, immutable request IDs
and timestamps, retry leases and verified release evidence. Verified completion
atomically cancels the planned decrease and refreshes the unchanged subscription
snapshot; existing paid grants remain intact. Pending withdrawals wake the billing
worker, receive a two-minute lease and run before scheduled-decrease observation.
Unsafe or conflicting provider observations enter review instead of issuing
another mutation. PostgreSQL tests cover ownership, repeated consent, stable retry
identity, late-response fencing, immutable evidence and preserved paid access.
Targeted Stripe getter/handler suites pass. Customer-facing withdrawal controls
and full provider HTTP lifecycle fixtures remain outstanding.

Confirmed decreases now wake the existing opt-in Stripe reconciliation worker.
Pending requests receive its two-minute lease and resume attachment followed by
verified phase scheduling; scheduled requests observe renewal transitions before
invoice recovery. The shared coordinator refuses unconfirmed/foreign requests
and preserves separate durable leases for each provider mutation. Local
PostgreSQL tests cover queue wake-up, lease timing, ownership and scheduled
pre-renewal no-ops, alongside the existing adapter/recovery tests. The worker
still requires `SERVICE_STRIPE_RECONCILIATION_ENABLED=true`; customer-facing
decrease controls and complete provider HTTP lifecycle fixtures remain pending.

The saved decrease screen now offers “Keep my current plan” for scheduled changes
before the renewal cutoff. Its CSRF-protected POST persists withdrawal consent and
runs the shared withdrawal processor. Pending/review-needed withdrawals hide
conflicting actions; confirmed release displays the continuing original quantity
and monthly price, with a link back to billing for cancellation or other changes.
HTTP/PostgreSQL fixtures cover controls, CSRF, unconfirmed-request rejection,
status rendering and replay of completed withdrawal. Full successful provider
HTTP lifecycle coverage and browser visual verification remain outstanding.

`TestAlertsStripeDecreaseHTTPPostgres` now exercises real account sessions,
handlers, PostgreSQL transactions and Stripe SDK HTTP serialization against a
local provider server. It follows billing selection through reviewed confirmation,
schedule attachment/update, withdrawal, and subsequent end-of-period cancellation.
Repeated confirmation/withdrawal sends exactly one mutation per step; forged
quantity/price form fields do not alter saved consent. The fixture verifies restored
cancellation controls, retained original quantity and unchanged paid-through time.
The complete targeted Stripe getter/handler suites pass with this fixture. This is
not a real Stripe test-mode run; live provider validation, a full renewal-boundary
fixture and browser visual verification remain outstanding.

Browser layout verification (production templates/shared CSS with sample data,
without the central navigation or live account session) covered billing management
at 390px, 320px and 1280px, scheduled decreases at 390px and withdrawn status at
320px. The check found a crowded quantity label/input and adjacent primary actions.
The billing form now stacks the labeled quantity field, separates its sections,
and uses an outlined cancellation action. The 320px page has no horizontal
scroll overflow; the original Ubuntu Bold Italic wordmark remains in place.
Local preview: `/private/tmp/alerts-decrease-preview.go` (stopped after checking).
Mobile artifact: `/private/tmp/alerts-billing-mobile.png`. Targeted Stripe handler
tests pass after the template/CSS changes. Full signed-in browser/provider
validation remains separate from this layout check.

`TestAlertsStripeDecreaseRenewalHTTPPostgres` covers the renewal boundary with
historical saved consent and the real Stripe SDK against a local HTTP provider.
The reconciler observes the scheduled two-to-one change, records one quantity
transition and grants one-slot grace for the failed renewal without creating paid
access. Subsequent verified payment creates exactly one one-slot entitlement,
settles the grace record as recovered, and preserves the old two-slot invoice for
idempotent replay. Repeated recovery does not duplicate invoices or transitions.
The full targeted Stripe getter/handler suites pass. Provider mutations are
rejected by this fixture: renewal recovery is read-only. Real Stripe test-mode
validation and production account/delivery/deployment checks remain outstanding.

Service order pages now offer print/save for provider-verified payments, including
order date, payment method, tax-inclusive totals and access-period dates. Pending
orders are explicitly labeled as not payment receipts and have no print action.
Print CSS removes navigation/actions and decorative card styling; the receipt
wordmark uses Ubuntu Bold Italic with touching plus signs. Template tests cover
verified/unverified presentation and escaping. Browser fixture checks used the
production template and styles at 390px and 320px, plus print media at 794px;
there was no mobile horizontal overflow and print controls were hidden. Fixtures
omit central navigation and real account/payment integration. Emailed receipts,
real provider validation and live signed-in browser checks remain outstanding.

`emails.ServiceReceiptMail` now prepares HTML and plain-text service receipt
messages from provider-verified paid order views, with included tax, access dates,
an account-protected print link and an Alerts management link. The recipient must
come from the saved order. The stable `service-receipt:{publicID}` job key reuses
the existing commerce mailer's deduplication convention. Builder tests cover
escaping, invalid/unpaid/refunded orders, recipient validation and URL validation.
Migration 126 queues one receipt per order in the verified-settlement transaction,
without backfilling historical payments. The worker requires
`SERVICE_RECEIPTS_ENABLED=true`, enabled mail and a configured mail endpoint.
It leases jobs for two minutes, fences stale responses by attempt number, and
retries with the same mailer key using exponential delays from five minutes to
six hours. After twelve attempts, delivery requires operator review. Invalid
receipt data also enters review; orders no longer verified and paid are cancelled.
MailOff leaves jobs untouched. Acceptance requires both a successful HTTP status
and the mailer's success response; acceptance does not prove inbox delivery.
Database and local HTTP fixtures cover settlement rollback, duplicate callbacks,
lease recovery, stale responses, retry delays, disabled mail and accepted-job
replay. No real receipt emails have been sent. Live provider and mailer validation,
live alert routing remain outstanding.

The existing protected Prometheus endpoint now exposes
`btcpp_web_service_receipt_jobs{state="pending|accepted|cancelled|review"}`
(four separate state series) and
`btcpp_web_service_receipt_oldest_pending_timestamp_seconds` (zero when empty).
These are current database gauges, not lifetime counters; account deletion may
reduce them. They contain no recipient, order, account or provider identifiers.
The existing business collector caches successful reads for two minutes and keeps
its last snapshot on failure, so alert evaluation must also check
`btcpp_web_business_metrics_collection_success == 1` and freshness of
`btcpp_web_business_metrics_last_success_timestamp_seconds`. Watch for review
counts above zero and nonzero oldest-pending timestamps more than 24 hours old.
The latter detects a disabled or stalled worker as well as persistent mail errors.
PostgreSQL tests compare aggregates before and after acceptance; collector tests
verify receipt labels omit unrelated identifiers. Production scraping, alert
threshold validation and routing to an operator remain unverified.

`ops/service-receipt-alerts.yml` provides warning rules for review jobs persisting
ten minutes and pending receipts older than a day for fifteen minutes. Both
require a successful business refresh less than five minutes old; pair them with
central scrape/collector-failure alerts so stale data is not silently ignored.
`promtool test rules ops/service-receipt-alerts.test.yml` verifies delays,
recovery, failed/stale refresh suppression and the empty-queue sentinel. Import
the rules into the monitoring deployment and configure an operator route before
enabling production receipts; the rule file alone does not deliver alerts.

### Receipt recovery

Migration 127 adds a bounded recovery budget and an audit table. From the central
repository, with DATABASE_URL configured for the intended environment, inspect:

```sh
go run ./cmd/service-receipts
go run ./cmd/service-receipts -order PUBLIC_ORDER_ID
```

The first command lists up to 100 pending/review jobs, oldest first; the second
can inspect any receipt state. Output includes timestamps and attempt numbers,
but no recipient or account ID. After investigating and fixing the cause, requeue
one reviewed, paid receipt using the attempt number from inspection:

```sh
go run ./cmd/service-receipts -order PUBLIC_ORDER_ID -requeue \
  -expected-attempt 12 -actor OPERATOR_NAME -reason 'Mailer configuration repaired'
```

This atomically records the actor/reason and grants twelve additional attempts.
Do not put personal data or secrets in the reason. Duplicate/stale requests,
active leases and accepted/cancelled receipts cannot be requeued. Attempt numbers
remain monotonic, and late pre-recovery responses cannot finish the new budget.
The saved recipient, order and stable mailer key remain unchanged. Requeueing
does not send mail directly; the enabled worker rechecks verified paid status
before sending. Mailer idempotency still needs live verification for ambiguous
timeouts. `make build-ops` produces `target/service-receipts`; the Dockerfile
builds this alongside the web binary. Inside the deployed container, use
`./target/service-receipts` with the same flags from `/app`, inheriting the
service's environment. The compiled binary's help and read-only lookup were
verified against the isolated database. This command does not start a web server,
poll releases or send mail. Docker is unavailable in the development environment,
so container build/runtime verification and a production recovery exercise remain
outstanding.


### Production activation checklist (October 23, 2026)

The production audit found a live Stripe key and active Stripe Tax settings.
There is no sample-tax fallback: receipts and formal invoices use the verified
provider invoice's recorded tax and total. The preview files are not application data.
Tax registration coverage must be maintained in Stripe for the seller's actual obligations;
the audit is not a determination of where registration is required.

The checked-in App Platform specification now enables
`SERVICE_RECEIPTS_ENABLED=true` and
`SERVICE_STRIPE_RECONCILIATION_ENABLED=true`.
For the existing app, merge these two runtime variables into its current specification
or set them in App Platform settings; do not replace the live specification wholesale
with this historical checked-in specification. Deploying source alone does not
necessarily synchronize App Platform environment settings.

The existing signed Stripe endpoint `https://btcpp.dev/callback/stripe` must retain
its existing subscriptions and also receive:
- `checkout.session.completed`
- `checkout.session.expired`
- `invoice.paid`
- `invoice.payment_failed`
- `customer.subscription.updated`
- `customer.subscription.deleted`
- `customer.subscription.paused`
- `customer.subscription.resumed`

Update the existing endpoint rather than replacing its signing secret. Verify that
its event API version supplies the invoice parent/subscription metadata expected by
the installed Stripe SDK. Before changing an existing endpoint's API version, verify
the merchandise and ticket callback fixtures too.

The audit found no queued receipt jobs. Enabling the receipt worker will send any
eligible jobs present at activation time, then future verified payment receipts.
Do not send an unpaid-checkout email or invent tax amounts to test the live path.
After an authorized real purchase, verify the signed `invoice.paid` callback, one
paid order with the provider's total and tax, one receipt job accepted by the mailer,
and an account-owned downloadable invoice. Check `service-receipts` and metrics
for failures; acceptance by the mailer is not proof of inbox delivery.

Production settings/webhook updates require explicit rollout authorization.

### Alerts checkout address minimization

The prepaid address form sends the address to Stripe Tax for calculation, but
Bitcoin++ does not retain it in Alerts checkout quotes, tax quotes, or tax
transactions. These records retain provider IDs, verified amounts, expiry, and
payment evidence; full provider response bodies and destination fields are not
stored. Migration 149 removes those fields from existing Alerts tax records and
enforces minimization for writes during a rolling deployment. Other products and
separately requested billing invoices are unchanged. This does not delete Stripe
records or historical backups; do not describe it as deletion from every system.

### Refunded card plan recovery

A full refund does not imply Stripe has ended the subscription: cancellation may
be scheduled for the end of the annual period. A blocked Bitcoin quote now explains
that distinction. When all recorded purchases are fully refunded without retained
access and Stripe confirms a scheduled cancellation, the user can explicitly agree
to end the card plan immediately. Consent is recorded before the provider call;
the cancellation disables invoicing and proration and is verified before opening
a new Bitcoin order. Active plans without scheduled cancellation, partial refunds,
retained access, unknown invoices and provider failures remain blocked. Fully
refunded Alerts receipts link to a fresh Bitcoin quote rather than reusing the old
card order. Other users' subscriptions are never changed in bulk.


## Direct Bitcoin checkout

The Bitcoin prepay page shows the account-derived inclusive price and access
period alongside the billing address and current terms. The server stores a
short-lived pricing preview in the session, without an address. Submission
validates that preview, selection, policy and billing revision, calculates the
included tax, confirms the durable order, and opens OpenNode directly. Separate
quote review and order-payment pages are recovery paths, not normal checkout
steps. Prorated increases show the actual added quantity and price before consent.

Provider handoff failures return to the same order. Repeated submissions resume
that purchase rather than creating another charge. Existing refund/cancellation
consent checks still apply when switching away from a card plan.

New annual card subscriptions already use one plan-and-terms page followed by
Stripe Checkout, which collects the billing address and card details. Existing
card-plan upgrades retain their dedicated prorated change review.
