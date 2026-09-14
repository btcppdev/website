# Hackathon judging

The functional port of PR #40 keeps the current site navigation and page structure, with judging-specific submission controls and confirmation styling.

- Judges explicitly submit or update their rankings. Empty ballots are rejected. Browser edits remain local until submission; navigation warns about unsaved edits.
- The first successful submission for a judge/round shows confetti once. Reloading the confirmation or updating a ballot does not replay it. Reduced-motion preferences suppress the animation.
- Sponsor-only judges see their sponsor awards without an unrelated regular ballot.
- When no round is active, admin scoring selects the latest completed round.
- Results remain available only for closed rounds and authorized judges/staff. Submitting does not unlock open-round standings.

## Streaming results

The results page uses EventSource at `/{conf}/hackathon/judging/results/live`. It receives rendered result snapshots over SSE. There is no two-second results polling.

Migration 101 installs transactional PostgreSQL notifications for changes to scores, deliberation order, events, projects, competitions, and judging/staff roles. Row triggers ignore unchanged rows and changes only to `updated_at`: timeline reads synchronize event metadata and must not cause notification loops. Identical notifications in a transaction are coalesced, and rollback emits none.

Each application instance opens one dedicated LISTEN connection while it has connected viewers. Result snapshots are shared per competition/round, with concurrent computations coalesced. Authorization remains per viewer and is checked before every update. A listener reconnect invalidates snapshots to recover missed updates. The database connection must support PostgreSQL session-level LISTEN; no new application environment variables are required.

Streams send 15-second heartbeats and renew after 60 seconds, before the web server's write timeout. Renewal reloads sessions and handles time-based state changes. Hidden tabs disconnect; visible tabs reconnect. Role removal stops access, and the client removes previously displayed results. The SSE session middleware avoids scs response buffering and never commits a long-running stream's session over a concurrent browser request.

The application sends `X-Accel-Buffering: no` and `Cache-Control: private, no-store, no-transform`. A production rollout still needs a proxy-level smoke test to verify immediate delivery and reconnect behavior; local tests do not verify DigitalOcean's proxy.

## Migration and history

Apply migrations 100 and 101 with the normal application migration process. Their numbers follow master at 099; renumber if another branch ships migrations first.

Migration 100 backfills historical scorecards into submission history. Existing autosaved ballots therefore count as previous submissions. First/last timestamps are audit history; current ballot presence continues to derive from ranked scorecards.

Account merging transfers submission history through the existing merge manifest. For duplicate judge/round records, the canonical record remains and the source record is retained in the undo manifest. Undo restores both original records.

## Verification

Affected handler/getter/web package tests pass. Race-enabled shared-snapshot, SSE/session, real PostgreSQL notification, ballot persistence, and merge/undo tests pass against a disposable database. Notifications are tested for real changes, unchanged timeline reads, and rollback. SSE tests cover anonymous access, open-round denial, external-connection updates, and judge-role removal.

Browser verification covered desktop, 390px and 320px mobile layouts, submission/update, one-time confetti, and results streaming/reconnection. The fixture intentionally omits unrelated navigation/account and live-broadcast status endpoints.

Run the fixture only against a disposable, migrated database:

```sh
BTCPP_POSTGRES_SMOKE=1 JUDGING_BROWSER_PREVIEW=1 DATABASE_URL='<local test database>' \
  go test ./internal/handlers -run '^TestJudgingSSEFlow$' -v -timeout 0
```

Open http://127.0.0.1:8094/preview. The fixture's `/preview/close` route closes the round and opens results.

An existing broader schema test reports three unrelated badge relationships missing from the person-merge manifest: `organization_badge_grants.recipient_person_id`, `organization_badge_grants.created_by_person_id`, and `person_badge_presentations.person_id`. This port registers ballot history; it does not change badge merging.
