#!/bin/sh
set -eu

# Invoke in a clean Nix shell; preserve its SDK variables for race builds.
# Never reuse or reset a caller-supplied database.
cd "$(dirname "$0")/.."
if [ -e .env ]; then
    echo "Use a checkout without .env for acceptance tests." >&2
    exit 1
fi
case "${ACCEPTANCE_PGPORT:-}" in
    ''|*[!0-9]*) echo "ACCEPTANCE_PGPORT must name the local test PostgreSQL port." >&2; exit 1 ;;
esac
if [ "$ACCEPTANCE_PGPORT" -lt 1 ] || [ "$ACCEPTANCE_PGPORT" -gt 65535 ]; then
    echo "ACCEPTANCE_PGPORT must be between 1 and 65535." >&2
    exit 1
fi
case "${ALERTS_TEST_BINARY:-}" in
    /*) test -x "$ALERTS_TEST_BINARY" || { echo "ALERTS_TEST_BINARY is not executable." >&2; exit 1; } ;;
    *) echo "ALERTS_TEST_BINARY must be an absolute executable path." >&2; exit 1 ;;
esac
for tool in createdb go sqlite3; do
    command -v "$tool" >/dev/null || { echo "$tool is required; use the Nix development shell." >&2; exit 1; }
done

# Fixed loopback destination/user; ignore inherited libpq connection settings.
unset PGSERVICE PGSERVICEFILE PGOPTIONS PGHOSTADDR PGPASSWORD PGPASSFILE
database="commerce_account_deletion_acceptance_$(date -u +%Y%m%d%H%M%S)_$$"
export PGHOST=127.0.0.1 PGPORT="$ACCEPTANCE_PGPORT" PGUSER=btcpp PGDATABASE="$database"
export DATABASE_URL="postgres://btcpp@127.0.0.1:$PGPORT/$database?sslmode=disable"
export BTCPP_POSTGRES_SMOKE=1 CGO_ENABLED=1
createdb --no-password --host="$PGHOST" --port="$PGPORT" --username="$PGUSER" "$database"
echo "Acceptance database: $database (retained for inspection)"
go run ./cmd/db-migrate
# Packages share a schema and PostgreSQL notification channels. Keep packages
# sequential; the race detector and tests' explicit concurrency remain enabled.
go test -p 1 -race -shuffle=on -count=1 ./cmd/... ./internal/... ./external/...
