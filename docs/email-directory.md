# Central email directory and product integration

## Ownership and signup

bitcoin++ owns address identity (`email_contacts`), account associations
(`person_emails.contact_id`), and service/newsletter preferences
(`email_memberships`). A contact ID identifies an address, not a person. Existing
addresses may have no account, but new product signups create or link an account.

Insider signup stays on Insider. A single-use mailbox confirmation queues a durable
verified-account request to bitcoin++, which creates the account or returns its
existing owner. New accounts receive the existing default avatar and a placeholder
name; passwords and profile completion are not required. Signup selects only the
requested newsletter. Account provisioning retries after outages and delivery waits
for synchronization. Alerts continues using the existing bitcoin++ OAuth signup/login
flow; it never creates a separate identity database.

The public `/email-preferences` page is **management without signing in**, not an
accountless signup path. It emails a single-use 30-minute link, exchanged on POST for
a restricted 30-minute HttpOnly session. GET previews cannot change preferences.
Forms use CSRF protection and expected membership versions. Unknown addresses and
rate-limited requests receive the same message. Legacy contacts can use it too.

## Shared API

Service credentials are independent from OAuth, commerce and mailer secrets:

- bitcoin++ `.env`: `EMAIL_DIRECTORY_INSIDER_TOKEN`, `EMAIL_DIRECTORY_ALERTS_TOKEN`
- Insider `.env`: `EMAIL_DIRECTORY_TOKEN` matching the Insider credential;
  `BTCPP_BASE_URL` identifies the bitcoin++ origin.
- Alerts `.env`: `EMAIL_DIRECTORY_TOKEN` matching the Alerts credential;
  `BTCPP_OAUTH_BASE_URL` defaults to `https://btcpp.dev`.

Use distinct random values of at least 32 characters. Production email delivery
requires the product credential. Clients require HTTPS outside loopback, bound
request timeouts, limit response sizes and refuse redirects with credentials.

Authenticated endpoints beneath `/api/internal/email-directory/{service}`:

| Method/path | Purpose |
| --- | --- |
| `POST /resolve` | Resolve/create address identity; grants no consent or ownership |
| `GET /contacts/{id}` | Read contact and only this service's memberships |
| `PUT /contacts/{id}` | Idempotent batch of scoped preference changes |
| `POST /verified-account` | Insider-only mailbox-verification attestation; create/link an account |

A preference change supplies `newsletter`, `subscribed`, `expected_version`,
`delivery_mode`, optional `paused_until`, and an `Idempotency-Key` header.
`import_only=true` seeds missing memberships without replacing existing choices.
A successful replay never reapplies an earlier opt-in. Version conflicts reject
stale writes. Services cannot select another service's preference scope. The account
endpoint requires the private service credential and a stable confirmation proof ID;
it is not a public email-to-account lookup and returns no bitcoin++ login token.

## Delivery and synchronization

Alerts release email uses `alerts / releases`. Insider uses its existing stream IDs
under service `insider`. Other central lists use `btcpp / {list}`. The retired central
`insider` list is ignored; it is never imported as a new Insider stream.

Each product stores a projection and durable outbound changes in its own database.
Workers reconcile central choices, with compare-and-swap versions and request keys.
Projection writes cannot generate another outgoing change. Stale opt-ins lose to
central decisions; conflicting explicit opt-outs are retried against the current
version. Contacts are imported lazily as the workers encounter existing subscribers.
Local unsubscribe paths continue to stop local delivery during central outages.

Release/campaign workers check central consent before passing each message to the
mailer. An outage or unsynchronized choice holds delivery for retry. Polling central
choices also queues cancellation of accepted mail using the existing mailer workers.
bitcoin++ runs a durable cancellation worker for its own newsletter queue. Messages
already being delivered cannot be recalled. Transactional receipts/account messages,
Telegram settings, project selections and paid access remain separate.

Old preference and unsubscribe links remain usable. Insider preference forms reject
stale full-form submissions rather than restoring other stream choices accidentally.
Account deletion erases the authoritative address, revokes management tokens and
retains pseudonymous opt-out history. Cancellation address snapshots are cleared
when the mailer acknowledges cancellation. Product deletion removes directory links
and pending account-provisioning records.

## Migration and rollout

1. Run `go run ./cmd/email-directory-audit` from bitcoin++ with `DATABASE_URL` in
   root `.env` or the environment. It prints counts only in a read-only transaction.
   Exit 2 indicates address/consent conflicts needing review; exit 1 is an audit error.
2. Deploy bitcoin++ migrations 129 and 130 and its API before updating product workers.
   Migration 129 backfills central addresses/memberships, preserving known opt-outs
   and leaving unknown legacy consent timestamps unset. Both migrations are atomic.
3. Configure the distinct service credentials in each root `.env` and deploy Insider
   (including migration 002) and Alerts. Verify successful synchronization before
   enabling production campaign/release delivery.
4. Exercise real signup, management-link delivery, scoped unsubscribe, queued-mail
   cancellation and account deletion with test accounts before launch.

The central audit does not inspect the separate Insider database or the mailer's
suppression store. Product workers seed existing product preferences without
superseding central opt-outs. Duplicate editable legacy address columns remain
compatibility mirrors while the old bitcoin++ account/newsletter callers still use
them; contact IDs are the integration identity. Receipts retain historical address
snapshots. Bounce/suppression outcomes continue to be enforced by the mailer; a
central bounce-event history is a separate follow-up.

No production migration, import, real-email rehearsal or deployment has been run
as part of this implementation.
