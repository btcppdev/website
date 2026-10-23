# Local shared-service acceptance

`make test-acceptance` creates a fresh database, applies the normal migrations,
and runs command, internal and external packages with PostgreSQL fixtures, race
detection and randomized test order. Packages run sequentially because fixtures
share database-wide notification channels and serializable transactions.

Use a disposable local PostgreSQL server with a `btcpp` role. The runner always
connects to `127.0.0.1`, requires an explicit port, and generates its own database
name. It never resets an existing database. It leaves each new database for
inspection; remove it manually when no longer needed. Do not run two acceptance
processes against the same generated database.

Build the Alerts executable from the corresponding Alerts checkout first. The
runner requires its absolute executable path, so the connected account-deletion
rehearsal cannot silently skip. The central Nix shell includes PostgreSQL and
SQLite command-line tools used by these tests.

Start the test cluster outside temporary storage, for example from this checkout:

```sh
# Run these setup commands inside nix develop, once for a new cluster.
initdb -D target/acceptance-postgres -U btcpp -A trust --no-locale -E UTF8
pg_ctl -D target/acceptance-postgres -l target/acceptance-postgres.log \
  -o '-h 127.0.0.1 -p 55483 -k /tmp' start
```

Clear inherited credentials before entering Nix (clearing afterward removes SDK
variables required by the macOS race build). From this central checkout:

```sh
env -i HOME="$HOME" PATH="$PATH" nix develop -c sh -c '
  ACCEPTANCE_PGPORT=55483 \
  ALERTS_TEST_BINARY=/absolute/path/to/alerts/target/alertsd \
  make test-acceptance
'
```

The runner rejects a checkout containing `.env` because the migration command
loads it. Use an isolated worktree without runtime credentials. It validates the
port, executable and command dependencies before creating the database. It also
replaces inherited database settings with its loopback fixture settings.

These tests exercise local mocks, not real Stripe/OpenNode payments, mailer
inbox delivery, Telegram or Nostr publishing. Optional tests with other explicit
fixture flags remain separate. A passing run does not complete browser,
production deployment, monitoring or backup/restore launch checks.

## Release build and Bitcoin payout configuration

`make build build-ops` builds the web server and all five shared-service operator
commands. The Dockerfile invokes those same targets. Local `.env` and `.env.*`
files are excluded from the Docker build context; supply runtime values through
the deployment's secret environment rather than baking them into the image.

Recorded Bitcoin payouts use these runtime settings:

| Setting | Purpose |
| --- | --- |
| `BITCOIN_RPC_URL` | Trusted Bitcoin Core RPC endpoint, with credentials supplied separately. |
| `BITCOIN_RPC_USER` | RPC username. |
| `BITCOIN_RPC_PASSWORD` | RPC password, provided through the deployment secret store. |
| `BITCOIN_RPC_CHAIN` | Defaults to `main`; production rejects any other chain. |
| `SERVICE_PAYMENT_RECONCILIATION_ENABLED` | Set to `true` to run payment, refund-tax and payout follow-up workers. |

The admin verification forms need RPC configuration even when the background
worker is disabled. Follow-up monitoring starts only when the shared worker is
enabled and node configuration is valid. Stripe Tax also requires `STRIPE_KEY`;
OpenNode payment recovery requires `OPENNODE_KEY`. Refund access/receipt delivery
continues to use the separate shared fulfillment configuration.

Install both `ops/service-refund-tax-alerts.yml` and
`ops/bitcoin-payout-alerts.yml` alongside the existing refund-delivery rules on
the monitoring service. Confirm scrape freshness and route warnings to operators.
A compiled binary and passing local rules are not proof that these deployment
settings or alert routes are installed.

October 1 build checkpoint: `make build build-ops` passed
(`/private/tmp/btcpp-service-release-build-20261001.log`). The same six commands
also compiled with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`; `file` confirmed all
six outputs under `target/release-linux-amd64/` are statically linked Linux x86-64
executables (`/private/tmp/btcpp-service-linux-build-20261001.log`). These are
local build artifacts, not a deployed release. Docker is unavailable in this
session, so the image build and container startup remain unverified.

## Alerts payment settings

The payment-settings update adds migrations 133 and 134. Apply these with the
normal database migration command before starting the updated bitcoin++ app.
The matching Alerts update accepts the new optional `scheduledEntitlements`
field and enforces its start dates locally, avoiding a polling gap at renewal.
Deploy Alerts before or alongside bitcoin++; older clients safely ignore the
optional field and continue polling. No new OAuth scope is required.
`SERVICE_RECEIPTS_ENABLED=true` and the existing mailer configuration also
start prepaid renewal reminders (seven days and one day before access expires).

Local verification uses an isolated migrated PostgreSQL database and mocked
provider HTTP, never production charges:

- Update saved card: Stripe Checkout setup mode, followed by an owned,
  CSRF-protected confirmation. Verify that only the Alerts subscription's default
  payment method changes; quantity, period, and cancellation stay unchanged.
- Card setup completion/expiry webhooks are acknowledged separately from tickets
  and subscription purchases. They grant no access and do not apply a card.
- Switch to Bitcoin: confirm cancellation, wait for verified Stripe cancellation,
  and purchase a 12-month term beginning at the current card period boundary.
  If the previous period already ended, a fresh quote begins now.
- A switch can be withdrawn before payment is initiated. An unstarted order is
  canceled transactionally; an initiated or paid OpenNode charge blocks automatic
  card resumption and needs support review to avoid overlapping payments.
- An expired, unstarted Bitcoin order can be canceled and requoted. Initiated
  charges reuse the existing order/reconciliation flow, never a second charge.
- Unpaid expiry falls back to three projects without renewal-failure grace.
  Verified future-dated grants do not become active before their start date.
- Prepaid renewal reminders link back to account-owned checkout. Renewals retain
  the prepaid boundary; duplicate clicks return the existing purchase. Reminders
  stop once a subsequent term is funded. The mailer job key is stable on retry.

Before production release, exercise one Stripe test-mode card update and one
OpenNode sandbox switch payment, including returning after browser interruption.

### Stripe service refunds

Apply migration `135_service_stripe_refund_sync.sql`. Add `refund.created`,
`refund.updated`, `refund.failed`, and `charge.refunded` to the existing signed
Stripe webhook. Keep `SERVICE_PAYMENT_RECONCILIATION_ENABLED=true` and the
production Stripe key configured for missed-event recovery. The integration
only reads Stripe; it never initiates refunds.

The worker starts at application startup, claims up to 20 settled service
payments per pass, and runs every minute. Successful checks are scheduled one
hour later; errors retry after five minutes and abandoned claims expire after
ten minutes. Historical settled payments are included without relying on Stripe
webhook retention. Recorded full refunds are excluded from polling. Webhooks
and polling use the same verified, idempotent ledger.

Pending, failed, or canceled refunds do not change accounting. Successful
partial/full refunds update order status and totals. This covers Alerts and
Insider service payments; merch and ticket refund accounting remain separate.
Admin service-order pages display refunded/remaining amounts and link to refund
review. The merch refund form is hidden and rejected server-side for services.

Existing access policy applies: Alerts does not count refunded purchases until
an admin records a retain-access decision; retention preserves original expiry.
Insider reversal delivery waits for an access decision and original settlement
acknowledgment. Refunds do not cancel recurring subscriptions.

Before rollout, rehearse a partial refund followed by a full refund in Stripe
test mode, repeated webhooks, pending-to-successful transitions, and recovery
with webhook delivery absent. Verify original totals remain unchanged and each
refund is counted once. Local automated tests use mocked Stripe with PostgreSQL;
no real refund has been initiated during development.
