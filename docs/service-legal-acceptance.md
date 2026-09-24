# Shared service checkout policy acceptance

Initial Alerts Stripe subscriptions now support explicit policy acceptance.
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

- OpenNode prepaid confirmation, prorated upgrades and Insider checkout acceptance.
- Copy the original agreement reference into downstream order/receipt views where
  needed; it currently remains reachable through the checkout record.
- Reviewed policy publication/activation tooling and cross-service consistency.
- Decide and implement handling of legacy open checkouts without acceptance before
  activating policies in an environment that already issued hosted payment URLs.
- Full browser/provider rehearsal and final reviewed policy text.

When no policy is active, existing prelaunch checkout behavior remains available.
This is not a completed legal launch gate. Existing subscriptions and automatic
renewals are not retroactively asserted to have accepted a newly published policy.
The tables extend the unmerged consolidated migration 128; verification uses fresh
databases, with no migration checksum or hook bypass.
