# Email directory release check — 2026-10-22

The integration branch `codex/alerts-current` was rebased onto production master
`e893bd6f`, preserving the Go 1.27.3 / Alpine 3.24 runtime and CA trust updates.
No merge commits were created. Production deployment remains pending.

## Local verification

- Migration prefix hook against `origin/master`: passed.
- Fresh local PostgreSQL database: all migrations through 130 passed.
- Upgrade rehearsal: initialized a second database using master migrations through
  116, added synthetic account and contact-only newsletter records, ran the
  read-only directory audit, then applied 128–130. Repeat migration startup was
  a no-op. No applied migration checksum was changed.
- Tests passed for internal/emaildirectory, internal/mtypes, internal/missives,
  internal/handlers, internal/db, and cmd/web.
- Database-backed external/getters suite passed after fixing the person-merge
  manifest to move and restore verified email account proofs. Email-directory
  PostgreSQL integration tests also passed.
- Web binary built successfully with Go 1.27.3.

## Production observations and remaining steps

DigitalOcean app `btcpp` serves `btcpp.dev` from `btcppdev/website` master with
push-triggered deployment. Database `db-pgsql-btcpp` is online; managed backups
include 2026-10-21 23:43 UTC. The available API token lists resources and backups,
but database connection/user responses omit credentials and app console access
returns HTTP 403. The expired saved `btcpp` context returns HTTP 401.

A temporary single-IP database firewall rule added during diagnosis was removed;
the original rules remain. No production schema or application was changed.

Before pushing master:

1. Restore operator access to database credentials and App Platform console/deploy.
2. Run the read-only production email-directory audit and resolve any conflicts.
3. Verify the backup/restore point and configure distinct directory service tokens.
4. Recheck upstream master, then fast-forward/push the reviewed release; startup
   applies migrations under the existing lock/checksum runner.
5. Confirm migration completion, application health, preference page, authentication,
   and service API authorization. Test actual delivery with approved test recipients.

Alerts and Insider OAuth setup and product deployment remain separate work.
