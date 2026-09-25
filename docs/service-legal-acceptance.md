# Shared service checkout policy acceptance

Initial Alerts Stripe subscriptions, prorated card upgrades and stored prepaid quote confirmation now
support explicit policy acceptance.
`service_legal_revisions` holds immutable, product-specific reviewed text and its
content hash. `service_legal_current` selects a revision; registration alone does
not make it active. No launch policy is seeded or activated by the migration.

The initial card review renders the actual central snapshot, escaped as text,
and requires an unchecked Terms agreement/Privacy acknowledgment. The handler
checks account ownership and CSRF, then checks the submitted version and checkbox.
Reservation rechecks the active revision under the same advisory lock used by
activation, before committing the frozen checkout intent. This closes the race
between form review and a policy update.

`alertsstripe.Intent.LegalAcceptance` contains version, exact Terms/Privacy text,
SHA-256 and server acceptance time. It is retained in the existing immutable
checkout JSON. No IP address or browser fingerprint is added. An exact retry
preserves the original evidence; a different accepted version cannot rewrite an
existing intent. The normal saved checkout return path can continue an already
accepted purchase under its original terms.

The content hash uses SHA-256 of the JSON array `[termsText, privacyText]`, matching
Alerts' policy snapshot format. Operators must register the same reviewed text and
version published on the product site. The review page displays the central text
so agreement does not rely on a mutable external link. Operator publication and
cross-service rollout tooling remain to build; the exported registration and
activation functions are not public HTTP endpoints.

## Prepaid orders

The quote review page loads the current policy and requires explicit agreement.
Confirmation rechecks the revision under the activation lock and freezes the
server-created acceptance in the purchase quote snapshot, in the same transaction
as the order and tax evidence. Caller-supplied acceptance objects are rejected.
The idempotency comparison preserves all pricing/tax checks while retaining the
first acceptance time; submitting another version cannot rewrite an existing order.

An already-confirmed quote redirects to its account-owned order. That order and
its receipt show the original policy version, acceptance time and escaped policy
text. A later policy update does not relabel the purchase. This is separate from
starting payment; confirmation still creates no provider charge.

## Prorated Stripe upgrades

Upgrade review displays the current policy with an unchecked agreement box.
Confirmation stores the exact policy text, hash and server timestamp in
`alerts_stripe_increases.legal_acceptance`, atomically with the authorization and
before provider mutation. Activation and confirmation share the policy lock.
The existing immutable quote still determines the amount and proration date.
The database rejects changes to the saved acceptance; retries must supply its
original version even after policy replacement. Existing confirmed upgrades can
continue through recovery and invoice payment without authorizing a second charge.

## Stripe order and receipt evidence

Newly recorded Alerts subscription invoices copy acceptance from the immutable
checkout intent into the order quote. Renewals retain that same original evidence
and timestamp; they do not claim fresh acceptance of the current policy. Prorated
upgrade invoices instead copy the upgrade confirmation's own immutable agreement.
The existing account-owned order/printable receipt page displays its version,
acceptance time and escaped Terms/Privacy text. Legacy purchases without recorded
agreement remain without one; this change does not invent historical consent or
rewrite existing immutable orders. Receipt emails continue linking to the order.

## Verification

Fresh normal migrations, then all command/internal/external race suites were run.
A duplicate primary email in the new database fixture was corrected; full handler
and getter suites then passed with `-p 1`. Local Stripe SDK HTTP fixtures verify
account/CSRF protection, escaped text, required agreement, stale-version rejection
and a single provider session across retries. PostgreSQL tests cover immutable
snapshots/intents, no reservation on rejected agreement, future/rollback rejection,
product isolation and preservation of first acceptance after policy replacement.
No live Stripe calls or real payments occurred.

## Remaining rollout work

- Insider checkout acceptance; provider/browser
  rehearsals of all prepaid terms and prepaid increase paths.
- Decide whether future renewal policy changes need a separate reacceptance flow;
  existing renewals retain the original subscription agreement.
- Reviewed policy publication/activation tooling and cross-service consistency.
- Decide and implement handling of legacy open checkouts without acceptance before
  activating policies in an environment that already issued hosted payment URLs.
- Full browser/provider rehearsal and final reviewed policy text.

When no policy is active, existing prelaunch checkout behavior remains available.
This is not a completed legal launch gate. Existing subscriptions and automatic
renewals are not retroactively asserted to have accepted a newly published policy.
The tables extend the unmerged consolidated migration 128; verification uses fresh
databases, with no migration checksum or hook bypass.

Prepaid follow-up (September 24): full handler/getter race suites passed. Tests
reject missing/stale agreement without creating orders, preserve evidence across
policy changes and retries, reject caller-forged acceptance and forbid stored
snapshot changes. The real-session HTTP fixture covers required escaped review,
OpenNode order/payment flow and duplicate confirmation. A targeted final handler
run verifies confirmed-quote redirects and the policy text in order/receipt views.
Logs: `/private/tmp/alerts-prepaid-legal-20260924.log` and
`/private/tmp/alerts-prepaid-legal-view-20260924.log`. Providers remain local mocks.

Prorated card upgrade follow-up (September 24): fresh migration 128 and all
command/internal/external race suites passed in
`commerce_account_deletion_acceptance_20260924235759_20304`. The final HTTP
regression also passed, covering escaped policy text, required agreement, no
Stripe update on missing/stale acceptance, repeated confirmation, invoice payment
and worker recovery. PostgreSQL coverage verifies concurrent reservation, policy
replacement, unchanged first acceptance and database immutability. Logs:
`/private/tmp/alerts-upgrade-legal-20260924.log` and
`/private/tmp/alerts-upgrade-legal-http-20260924.log`. Connected account-deletion
tests used the existing September 23 Alerts executable; this run does not verify
the later Alerts signup policy UI. No real payments occurred.

Card receipt follow-up (September 24): full getter race suite passed, including
initial invoice, renewal and prorated upgrade agreement preservation after a
policy replacement. The real-session HTTP upgrade fixture also passed its new
paid receipt assertion (version, escaped text and print/save control). Logs:
`/private/tmp/alerts-card-receipt-legal-20260924.log` and
`/private/tmp/alerts-card-receipt-http-20260924.log`. No provider calls or real
charges were made; provider responses were local fixtures.
