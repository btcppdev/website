# Minimal Local Dev Environment Plan

## Goal

A clean checkout can run one command from inside `nix develop` and get a usable local app at `http://localhost:8888`, backed by local Postgres, with a seeded admin login.

## Non-Goals

- No Docker Compose.
- No MinIO, LocalStack, or local S3 replacement.
- No production database copy.
- No real Stripe, OpenNode, Buffer, YouTube, X, Spaces, or mailer setup.
- No broad config refactor.
- No fixture system beyond the rows required to boot and navigate the core app.

## Required Work

### 1. Add Local Env Bootstrap

Add a tracked `.env.local.example` with dev-safe values:

```env
PROD=false
PORT=8888
HOST=localhost
DATABASE_URL=postgres://btcpp@127.0.0.1:55432/btcpp_dev?sslmode=disable
HMAC_SECRET=local-dev-only-change-me
MAILER_OFF=true
RECORDINGS_AUTOPUBLISH_ENABLED=false
X_UPLOADER_ENABLED=false
```

Add `just dev-bootstrap` that copies `.env.local.example` to `.env` only when `.env` does not already exist.

### 2. Add Minimal Database Seed

Add `cmd/dev-seed`.

It must be idempotent and only seed:

- One active conference, tag `dev26`.
- One conference day with at least one venue.
- One conference ticket.
- One admin person, email `dev-admin@example.test`.
- One `global-admin` role for that admin person.

Do not seed sponsors, recordings, social posts, volunteers, hotels, or payment rows in the first pass.

### 3. Add Dev Login Link Command

Add `cmd/dev-login`.

It should load `.env`, derive the HMAC key, and print a valid local login URL for:

```text
dev-admin@example.test
```

The URL should redirect to `/admin` after auth.

### 4. Add One Local Run Command

Add `just dev-up` that runs, in order:

```sh
just dev-bootstrap
make db-start
go run ./cmd/db-migrate
go run ./cmd/dev-seed
go run ./cmd/dev-login
make dev-run
```

`make dev-run` remains the foreground process.

## Acceptance Criteria

- From a clean checkout, inside `nix develop`, `just dev-up` starts the app without production secrets.
- `http://localhost:8888` renders.
- `http://localhost:8888/dev26` renders.
- The printed login URL signs in as `dev-admin@example.test`.
- `/admin` is accessible after using the printed login URL.
- Booting and rendering those pages does not require external network services.

