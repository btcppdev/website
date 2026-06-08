APP_NAME = btcpp-web
GO_ENV = CGO_ENABLED=0 GOSUMDB=sum.golang.org
BTCPP_PGROOT ?= $(if $(XDG_DATA_HOME),$(XDG_DATA_HOME)/btcpp-web/postgres,$(HOME)/.local/share/btcpp-web/postgres)
BTCPP_PGDATA ?= $(BTCPP_PGROOT)/data
BTCPP_PGRUN ?= $(BTCPP_PGROOT)/run
BTCPP_PGLOG ?= $(BTCPP_PGROOT)/postgres.log
PGHOST ?= 127.0.0.1
PGPORT ?= 55432
PGDATABASE ?= btcpp_dev
PGUSER ?= btcpp
DATABASE_URL ?= postgres://$(PGUSER)@$(PGHOST):$(PGPORT)/$(PGDATABASE)?sslmode=disable

export BTCPP_PGROOT BTCPP_PGDATA BTCPP_PGRUN BTCPP_PGLOG PGHOST PGPORT PGDATABASE PGUSER DATABASE_URL

.PHONY: dev-run
dev-run: build-all db-start
	air -build.bin target/$(APP_NAME) -build.cmd="make build-all"

.PHONY: run-dev
run-dev: dev-run

.PHONY: db-start
db-start:
	@command -v initdb >/dev/null || (echo "initdb not found; run this inside nix develop"; exit 1)
	@command -v pg_ctl >/dev/null || (echo "pg_ctl not found; run this inside nix develop"; exit 1)
	@command -v createdb >/dev/null || (echo "createdb not found; run this inside nix develop"; exit 1)
	@mkdir -p "$$BTCPP_PGDATA" "$$BTCPP_PGRUN"
	@if [ ! -s "$$BTCPP_PGDATA/PG_VERSION" ]; then \
		initdb --auth=trust --username="$$PGUSER" --pgdata="$$BTCPP_PGDATA" >/dev/null; \
	fi
	@if ! pg_ctl -D "$$BTCPP_PGDATA" status >/dev/null 2>&1; then \
		pg_ctl -D "$$BTCPP_PGDATA" \
			-l "$$BTCPP_PGLOG" \
			-o "-k $$BTCPP_PGRUN -p $$PGPORT -c listen_addresses=127.0.0.1" \
			start; \
	fi
	@createdb -h "$$PGHOST" -p "$$PGPORT" -U "$$PGUSER" "$$PGDATABASE" >/dev/null 2>&1 || true

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
db-pull-sanitized: db-start
	@test -n "$$PROD_DATABASE_URL" || (echo "PROD_DATABASE_URL is required"; exit 1)
	@test -n "$$DATABASE_URL" || (echo "DATABASE_URL is required; run this inside nix develop"; exit 1)
	@test -n "$$PGHOST" || (echo "PGHOST is required; run this inside nix develop"; exit 1)
	@test -n "$$PGPORT" || (echo "PGPORT is required; run this inside nix develop"; exit 1)
	@test -n "$$PGUSER" || (echo "PGUSER is required; run this inside nix develop"; exit 1)
	@test -n "$$PGDATABASE" || (echo "PGDATABASE is required; run this inside nix develop"; exit 1)
	@command -v pg_dump >/dev/null || (echo "pg_dump not found; run this inside nix develop"; exit 1)
	@command -v pg_restore >/dev/null || (echo "pg_restore not found; run this inside nix develop"; exit 1)
	@command -v psql >/dev/null || (echo "psql not found; run this inside nix develop"; exit 1)
	@set -e; \
	dump_file="$${TMPDIR:-/tmp}/btcpp-prod-$$(date +%Y%m%d%H%M%S).dump"; \
	trap 'rm -f "$$dump_file"' EXIT INT TERM; \
	echo "Dumping production database..."; \
	pg_dump "$$PROD_DATABASE_URL" --format=custom --no-owner --no-privileges --file "$$dump_file"; \
	echo "Resetting local database..."; \
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
