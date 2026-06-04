APP_NAME = btcpp-web
GO_ENV = CGO_ENABLED=0 GOSUMDB=sum.golang.org

.PHONY: dev-run
dev-run: build-all
	air -build.bin target/$(APP_NAME) -build.cmd="make build-all"

.PHONY: build
build:
	$(GO_ENV) go build -v -o target/$(APP_NAME) ./cmd/web/main.go

.PHONY: css-build
css-build:
	tailwindcss -i templates/css/input.css -o static/css/mini.css --minify

.PHONY: png-conv
png-conv:
	cd media && ./convert.sh $(conf) $(subdir) && rm $(conf)/$(subdir)/*.pdf && cd ..

.PHONY: build-all
build-all: build css-build

.PHONY: test
test:
	$(GO_ENV) go test ./...

.PHONY: db-pull-sanitized
db-pull-sanitized:
	@test -n "$$PROD_DATABASE_URL" || (echo "PROD_DATABASE_URL is required"; exit 1)
	@test -n "$$DATABASE_URL" || (echo "DATABASE_URL is required; run this inside nix develop"; exit 1)
	@test -n "$$PGHOST" || (echo "PGHOST is required; run this inside nix develop"; exit 1)
	@test -n "$$PGPORT" || (echo "PGPORT is required; run this inside nix develop"; exit 1)
	@test -n "$$PGUSER" || (echo "PGUSER is required; run this inside nix develop"; exit 1)
	@test -n "$$PGDATABASE" || (echo "PGDATABASE is required; run this inside nix develop"; exit 1)
	@command -v btcpp_pg_start >/dev/null || (echo "btcpp_pg_start not found; run this inside nix develop"; exit 1)
	@command -v pg_dump >/dev/null || (echo "pg_dump not found; run this inside nix develop"; exit 1)
	@command -v pg_restore >/dev/null || (echo "pg_restore not found; run this inside nix develop"; exit 1)
	@command -v psql >/dev/null || (echo "psql not found; run this inside nix develop"; exit 1)
	@dump_file="$${TMPDIR:-/tmp}/btcpp-prod-$$(date +%Y%m%d%H%M%S).dump"; \
	trap 'rm -f "$$dump_file"' EXIT INT TERM; \
	echo "Dumping production database..."; \
	pg_dump "$$PROD_DATABASE_URL" --format=custom --no-owner --no-privileges --file "$$dump_file"; \
	echo "Resetting local database..."; \
	btcpp_pg_start; \
	dropdb --if-exists --host "$$PGHOST" --port "$$PGPORT" --username "$$PGUSER" "$$PGDATABASE"; \
	createdb --host "$$PGHOST" --port "$$PGPORT" --username "$$PGUSER" "$$PGDATABASE"; \
	echo "Restoring local copy..."; \
	pg_restore --exit-on-error --no-owner --no-privileges --dbname "$$DATABASE_URL" "$$dump_file"; \
	echo "Sanitizing local copy..."; \
	psql "$$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/sanitize.sql; \
	echo "Local database refreshed and sanitized."

.PHONY: clean
clean:
	rm -f target/*
