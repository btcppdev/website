# Account deletion

Global administrators can open **Admin → People and identities → Delete an
account**, find the person, and review their identity and contribution counts.
The POST requires a CSRF token, typing `DELETE`, and confirming that the account
owner's request has been verified. Administrators cannot delete themselves.
Accounts with unresolved shared-email conflicts must have those conflicts
resolved first, so deleting one person does not erase another person's contact
records.

Migrations `108_deleted_accounts.sql` and `109_account_deletion_history.sql` are required. This feature has been tested
locally; no production account has been deleted or deployment performed.

## Historical attribution

Each deletion creates a **separate** person with a random five-digit alias such as `anon03910`. It contains
no copied profile or contact fields. Talks, project creators and memberships,
judging scores/votes, and award distribution records move to this person.
Other speakers, team members, project contents, rankings, and award amounts
remain intact. Separate placeholders preserve participant counts and avoid
collisions when two people from the same team or judging round are deleted.

Database triggers prevent editing the placeholder or granting it account
credentials, email aliases, roles, or organization access. Session creation
rejects missing/deleted accounts. Regular profile merges cannot use these
placeholders.

## Database removal

The source person is deleted in a serializable transaction. Foreign-key
cascades remove credentials, email aliases, roles, organization memberships,
consents, and other account-owned records. Historical contribution records
are explicitly reassigned before that deletion. Any unexpected restrictive
relationship causes the entire transaction to roll back.

The operation also clears known linked contact fields in registrations,
volunteer records, orders, addresses, shipping/tax response snapshots,
affiliate records, and ticket issuances. Registration tickets are revoked;
financial totals and payout statuses remain. Shared proposals retain setup/comments
and receive fresh invitation tokens. Proposals with no remaining live speakers
have their setup/comments and invitation tokens cleared. Private judging comments
and payout notes are cleared. Associated account invitations,
magic links, newsletter subscriptions, local email delivery records, and
recoverable merge snapshots are removed. Matching stored SCS sessions are
removed; any concurrently saved old session fails account-version validation.

This is intentionally **not** a normal person merge: normal merges preserve
login credentials and profile snapshots for undo. Account deletion has no undo
record containing the erased profile.

## Scope to review separately

This is not an automatic erasure of all published content or all services.
Review object-storage uploads (portraits, tax forms, and other documents),
mail-service queues and sent messages, payment/shipping provider records,
logs, backups, CDN/search caches, recordings, exported reports, and names
embedded in retained talk/project/editorial content separately. The review page
explains this before deletion. The transaction does not make remote storage or
provider calls and does not rewrite contributions by other people.

## Verification

`go test ./internal/handlers ./external/getters ./internal/auth ./internal/db`

With an isolated, fully migrated database:

```sh
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL='<isolated database>' \
  go test ./external/getters -run TestDatabaseSmokeDeletePerson -count=1 -v
```

The database tests verify preservation of talks, teams, judging, payout and
order totals; removal of credentials, contact snapshots, and merge recovery
data; distinct placeholders for two teammates; placeholder access prevention;
and rollback on an unexpected restrictive relationship. Handler tests verify
global-admin gating, CSRF rejection, and escaped review-page content.

Deleted accounts are excluded from `/whois` listings, profile lookups, and nested co-speaker/team credits. Event archives retain plain-text anonymous alias credits. Successful admin deletion clears the directory cache, including its stale fallback snapshot.

Deletion transfers project ownership to the oldest remaining live teammate (membership date, then ID). The anonymous contribution stays as a member. With no live teammates, no owner is assigned; hackathon managers and administrators retain project management access.

Tickets record their previous revocation status before anonymization. Reports use that historical status, while ticket access stays revoked. Repeated ticket imports skip anonymized registrations, and a database trigger protects their identity fields and disabled access from other updates. Shared proposal text needs manual review for embedded personal information.
