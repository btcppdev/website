set dotenv-load
set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

app := "btcpp-web"
goenv := "CGO_ENABLED=0 GOSUMDB=sum.golang.org"
dev_database_url := "postgres://btcpp@127.0.0.1:55432/btcpp_dev?sslmode=disable"

# Show available commands.
default:
  @just --list

# Run the app with live reload.
dev:
  make dev-run

# Alias for `dev`.
run:
  make dev-run

# Build the Go binary.
build:
  make build

# Build the Tailwind CSS bundle.
css:
  make css-build

# Build the app and CSS.
build-all:
  make build-all

# Run the compiled binary after building everything.
serve: build-all db-start
  DATABASE_URL="$${DATABASE_URL:-{{dev_database_url}}}" ./target/{{app}}

# Run all Go tests.
test:
  {{goenv}} go test ./...

# Run all Go tests with the race detector.
test-race:
  GOSUMDB=sum.golang.org go test -race ./...

# Run one package or test pattern, e.g. `just test-one ./internal/handlers TestAdmin`.
test-one pkg="./..." pattern=".":
  {{goenv}} go test -run '{{pattern}}' -count=1 -v {{pkg}}

# Run Go vet.
vet:
  {{goenv}} go vet ./...

# Format Go code.
fmt:
  go fmt ./...

# Tidy Go modules.
tidy:
  go mod tidy

# Download Go modules.
deps:
  go mod download

# Run the common local verification path.
check: fmt tidy css build test vet

# Configure this checkout to use the tracked git hooks.
hooks:
  git config core.hooksPath githooks

# Start the local dev Postgres database.
db-start:
  make db-start

# Show local dev Postgres status.
db-status:
  @BTCPP_PGROOT="$${BTCPP_PGROOT:-$${XDG_DATA_HOME:-$$HOME/.local/share}/btcpp-web/postgres}"; \
  BTCPP_PGDATA="$${BTCPP_PGDATA:-$$BTCPP_PGROOT/data}"; \
  pg_ctl -D "$$BTCPP_PGDATA" status

# Stop the local dev Postgres database.
db-stop:
  @BTCPP_PGROOT="$${BTCPP_PGROOT:-$${XDG_DATA_HOME:-$$HOME/.local/share}/btcpp-web/postgres}"; \
  BTCPP_PGDATA="$${BTCPP_PGDATA:-$$BTCPP_PGROOT/data}"; \
  if pg_ctl -D "$$BTCPP_PGDATA" status >/dev/null 2>&1; then \
    pg_ctl -D "$$BTCPP_PGDATA" stop; \
  else \
    echo "Postgres is not running at $$BTCPP_PGDATA"; \
  fi

# Open psql against the local dev database.
db-psql: db-start
  DATABASE_URL="$${DATABASE_URL:-{{dev_database_url}}}"; psql "$$DATABASE_URL"

# Drop, recreate, and migrate the local dev database.
db-reset: db-start
  @PGHOST="$${PGHOST:-127.0.0.1}"; \
  PGPORT="$${PGPORT:-55432}"; \
  PGUSER="$${PGUSER:-btcpp}"; \
  PGDATABASE="$${PGDATABASE:-btcpp_dev}"; \
  DATABASE_URL="$${DATABASE_URL:-postgres://$$PGUSER@$$PGHOST:$$PGPORT/$$PGDATABASE?sslmode=disable}"; \
  dropdb -h "$$PGHOST" -p "$$PGPORT" -U "$$PGUSER" --if-exists --force "$$PGDATABASE"; \
  createdb -h "$$PGHOST" -p "$$PGPORT" -U "$$PGUSER" "$$PGDATABASE"; \
  DATABASE_URL="$$DATABASE_URL" {{goenv}} go run ./cmd/db-migrate

# Apply local database migrations.
db-migrate:
  make db-migrate

# Replace local data with a sanitized production dump. Requires PROD_DATABASE_URL.
db-pull-sanitized:
  make db-pull-sanitized

# Replace local data with a production dump without sanitizing. Requires PROD_DATABASE_URL and ADMIN_BYPASS.
db-pull-unsanitized:
  make db-pull-unsanitized

# Clear the local disk cache.
cache-clear:
  rm -rf _cache

# Verify a magic-link URL against the local HMAC secret.
verify-magiclink url:
  {{goenv}} go run ./cmd/verify-magiclink -url '{{url}}'

# Backfill the speaker object manifest in Spaces.
speaker-manifest *args:
  {{goenv}} go run ./cmd/backfill-speaker-manifest {{args}}

# Convert media PDFs for a conference/subdir, e.g. `just png-conv berlin26 speakers`.
png-conv conf subdir:
  make png-conv conf={{conf}} subdir={{subdir}}

# Remove build output.
clean:
  make clean
