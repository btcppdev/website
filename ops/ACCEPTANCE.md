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
