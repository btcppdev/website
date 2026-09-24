# Shared service checkout policy acceptance

Initial Alerts Stripe subscriptions and stored prepaid quote confirmation now
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

- Stripe prorated upgrades and Insider checkout acceptance; provider/browser
  rehearsals of all prepaid terms and prepaid increase paths.
- Propagate the original card agreement reference into invoice/receipt views;
  prepaid order/receipt pages already expose their frozen agreement.
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
