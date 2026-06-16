# Local Postgres

Enter the Nix dev shell, then use the helper functions from `flake.nix`:

```sh
nix develop
btcpp_pg_start
btcpp_pg_migrate
btcpp_pg_psql
```

The default local connection string is exported as:

```sh
DATABASE_URL=postgres://btcpp@127.0.0.1:55432/btcpp_dev?sslmode=disable
```

The app now requires a working `DATABASE_URL` at startup because HTTP sessions
are stored in Postgres rather than a local Bolt file.

Data lives under `$XDG_DATA_HOME/btcpp-web/postgres` or
`$HOME/.local/share/btcpp-web/postgres` by default. This avoids permission
problems when the repo is checked out on a Windows-mounted WSL path. Override
`BTCPP_PGDATA`, `BTCPP_PGRUN`, or `BTCPP_PGLOG` if you want a different local
location. Stop the local server with:

```sh
btcpp_pg_stop
```

To rebuild the local database from scratch:

```sh
btcpp_pg_reset
```

## Schema Migrations

Migration files live in `db/migrations` and are applied in numeric prefix
order. The web app runs pending migrations automatically on startup when
`DATA_BACKEND=postgres`.

Applied migrations are tracked in the database in `schema_migrations`. Existing
databases that already have the initial schema are baselined as migration `001`
on first startup, then later migrations are applied normally. Migration SQL is
read from disk at runtime; deploys must include the checked-in `db/migrations`
directory alongside the app.

To run the same migration path manually:

```sh
make db-migrate
```

This repo includes a tracked pre-commit hook that rejects duplicate migration
number prefixes, such as adding a second `002_*.sql`. Enable it in a checkout
with:

```sh
git config core.hooksPath githooks
```

To replace the local database with a sanitized copy of production:

```sh
PROD_DATABASE_URL='postgres://...' make db-pull-sanitized
```

This target must be run inside `nix develop`. It dumps production to a
temporary custom-format archive, drops and recreates the local database,
restores the archive into the local Postgres instance, then runs
`db/sanitize.sql` to remove contact details, live invite tokens, calendar
notification IDs, source media URIs, notes, coupon codes, and ticket checkout
IDs. After the restore, it also applies any newer local migrations from
`db/migrations` and clears the local `_cache` directory so the next app start
fetches fresh data from the restored database.

To replace the local database with an unsanitized copy, you must provide the
admin secret whose SHA-256 matches the hardcoded allowlist digest:

```sh
PROD_DATABASE_URL='postgres://...' \
ADMIN_BYPASS='...' \
make db-pull-unsanitized
```

This skips `db/sanitize.sql` entirely and restores the production data as-is.
It still applies any newer local migrations from `db/migrations` and clears the
local `_cache` directory after restore. Use it carefully.

## Notion Import

The migration command imports the Notion-backed dataset into Postgres:

```sh
go run ./cmd/migrate-notion-postgres -reset -validate
```

Current import coverage includes conferences, conference days, ticket tiers,
discounts, registrations, affiliate usage, hotels, job types, volunteers,
volunteer info, work shifts, organizations, sponsorships, people, proposals,
speaker conference rows, scheduled conference talks, recordings, social posts,
newsletter subscribers, and missives.

`-reset` truncates the imported root tables and cascades through their child
and join tables before writing. Keep using it while iterating on migration data
so generated-UUID tables such as sponsorships, people, volunteers, work shifts,
hotels, and affiliate usages do not accumulate duplicate import rows.

Useful flags:

```sh
go run ./cmd/migrate-notion-postgres -dry-run
go run ./cmd/migrate-notion-postgres -database-url "$DATABASE_URL" -validate
go run ./cmd/migrate-notion-postgres -skip-speaker-confs -validate
go run ./cmd/migrate-notion-postgres -skip-conf-talks -skip-recordings -skip-social-posts -validate
```

The app can use Postgres locally by setting `DATA_BACKEND=postgres` in `.env`
or by exporting it in the shell. Exported shell variables take priority over
values loaded from `.env`.
