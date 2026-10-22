# Central email directory

## Implemented: foundation (migration 129)

`email_contacts` gives each normalized address an immutable UUID independent of
accounts. `person_emails.contact_id` preserves the existing account ownership,
verification and primary-address model. `subscribers.contact_id` connects the
mailing system to the same directory. Case and surrounding whitespace are
normalized; plus tags and dots are not removed.

`email_memberships` identifies preferences by `(contact_id, service, newsletter)`.
It retains explicit unsubscribed states. `email_preference_events` records each
transition and a monotonically increasing membership version. Existing memberships
have source `legacy-import` and no invented consent timestamp. Available opt-out
records are recovered from `newsletter_consent_requests`. The retired central
`insider` list is excluded, including its consent history. No new Insider stream
subscriptions are inferred from it.

During this additive transition, existing address columns remain compatibility
mirrors and legacy subscription writes are the only writers of `btcpp` memberships.
Database triggers ensure new account and subscriber rows acquire matching contacts
atomically. A one-way membership bridge maintains the directory in the same
transaction as existing subscribe/unsubscribe operations. Do not write `btcpp`
memberships directly until the legacy writers are replaced. The newsletter API
reads preferences through contact IDs. Unchanged selections create no new events.

Account registration does not reset an opt-out. An account address change references
a different contact; it does not rewrite the shared address. Changing a subscriber
address is rejected until an explicit preference-transfer flow exists. Account-link
deletions retain independent contacts and preferences. Contact erasure and account
deletion integration must be completed before deployment.

## Read-only audit

From this checkout, with `DATABASE_URL` in the root `.env` or environment:

```
go run ./cmd/email-directory-audit
```

This command runs a read-only repeatable-read transaction and prints counts only.
It works before migration 129, including databases without the new consent API
table from migration 128. Exit 0 means no detected address or preference conflicts;
exit 2 means conflicting addresses/preferences or empty addresses require review; exit 1 means the audit
could not run. No addresses or database credentials are printed. The migration
also rejects those conflicts rather than assigning ambiguous account ownership.

This report covers the central database, not Insider's separate database or the
mailer suppression store. `can_migrate` checks address normalization and active memberships conflicting with
the latest explicit opt-out; it is not launch approval.
Receipt, order, invitation and historical audit addresses remain snapshots; they
are not interpreted as newsletter consent. Do not deploy this foundation as the
completed cross-service directory.

## Remaining rollout

1. Inventory the remaining current delivery-address sources and historical snapshots.
   Import Insider contacts and stream preferences with a reviewed dry-run report;
   set Insider's existing `shared_contact_id` and preserve pauses/delivery modes.
2. Build the scoped contact/preference service, idempotent change requests and a
   durable delivery outbox. Replace legacy writers, then remove the one-way bridge
   and duplicate editable address columns. Keep old unsubscribe links working.
3. Add account-free preference management with hashed, expiring tokens, limited
   contact sessions, request throttling and non-enumerating responses. Use the same
   central membership writes for signed-in and account-free changes.
4. Connect Alerts and Insider delivery workers to current contact eligibility and
   preference events. A scoped unsubscribe affects only its service/newsletter;
   it does not cancel billing or disable Telegram. Reject stale updates and cancel
   queued sends where supported.
5. Integrate authenticated mailer outcomes, address-wide suppression, bounce history,
   recovery, account deletion, contact erasure and explicit verified email transfers.
6. Reconcile imports and test unsubscribe propagation, outages and retries before
   production deployment. No production directory audit or migration has run yet.

The existing legacy list retirement is separate from Insider the product: public
Insider pages, billing and the new stream subscriptions remain supported.
