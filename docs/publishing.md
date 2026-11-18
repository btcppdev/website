# Shared publishing

`/admin/publishing` is the global-admin calendar for all events. It combines local social deliveries, recording/YouTube release times, X broadcast reservations, and streamer snapshots. Week, month, agenda, queue and account views share event, account, type and status filters. Dates are displayed in the selected IANA timezone.

## First direct publishers

- X: OAuth 2 with PKCE, encrypted database credentials, text, up to four JPEG/PNG/WebP images (5 MB each), and one optional follow-up reply. Video announcements use the recording/broadcast workflow; direct video attachments are not implemented in this version.
- YouTube: the existing direct upload/release workflow remains authoritative. Use **Recordings & streams** to open any event's uploader or batch scheduler. The shared channel view edits its existing default slots, and scheduled recording cards link to the source entry.
- Instagram and LinkedIn: planned direct integrations. They appear as planned channels, cannot be connected or queued yet, and have no intermediary service. X and YouTube remain the first rollout phase.

## Enable direct X publishing

Configure these server secrets/settings:

```
PUBLISHING_ENABLED=false
X_OAUTH_CLIENT_ID=
X_OAUTH_CLIENT_SECRET=
X_OAUTH_REDIRECT_URL=https://btcpp.dev/admin/publishing/x-callback
SOCIAL_STATE_KEY=<base64-encoded 32-byte encryption key>
```

Register the exact callback in the X application. Connect the account in **Channels & schedules**, add weekday/time slots, and review its queue. New connections are paused. Set `PUBLISHING_ENABLED=true` and unpause the account when ready. The X application/account must have API publishing access and credit. Never paste client secrets into the browser or commit them to the repository. Keep the encryption key backed up; losing it requires reconnecting accounts.

The **Use for event X announcements** switch changes the existing speaker/talk/sponsor composer and new recording announcements to this account. Only one X account can own event routing. Pausing it holds new posts locally. There is no fallback publisher. Historical announcement records are retained to prevent duplicate posts during cutover, including uncertain outcomes.

## Queue guarantees

One database row represents one destination delivery. Select one or more X accounts in the composer to create separate items in a single transaction, linked by a shared campaign ID. Each copy has its own queue position, time, text, and delivery state; editing or rescheduling one copy leaves the others unchanged. Queue allocation serializes on the account row. Fixed-time deliveries retain their time, and queued posts fill recurring slots around them. A move-to-top or account schedule change reallocates queued posts. Spring-forward nonexistent times are skipped; fall-back times run once. Ordinary social posts can coincide with streams. Recording/broadcast scheduling retains its separate full-runtime plus five-minute conflict checks.

The dispatcher uses a database advisory lock across replicas, durable leases, and a persisted checkpoint for the root X post. Only explicit rate-limit rejections retry automatically (three attempts, increasing delays). Other rejected requests become failed deliveries. A timeout, 5xx, interrupted dispatch, or lost result after starting a non-idempotent request requires administrator review. Retrying a failed reply keeps the successful root. **Confirm published** requires checking the actual account and supplying the published post URL. **I checked: remaining post was not sent** explicitly permits resuming the unconfirmed step.

Draft, queued, or wholly failed posts can be edited with optimistic version checking. Sending or uncertain deliveries cannot be edited/canceled into an automatic retry. Published posts are never requeued implicitly. Canceling a local delivery does not remove an already published root.

## Rescheduling individual posts

Drag a scheduled X post (or its move handle) onto an available publication slot for that same account. Valid slots are highlighted during the drag. Other accounts, occupied slots, past times, and arbitrary calendar space are not valid destinations. The server validates the configured weekday/time in the account's timezone and serializes moves so two posts cannot claim one slot. Stale versions are rejected.

Click or keyboard-activate the move handle, or choose **Move to slot** in the post details or queue, to select from the next 90 days of available slots. This also works on mobile; touch-dragging uses the handle so scrolling the rest of the calendar still works. Escape cancels a drag.

A successful move pins only that delivery to the selected slot. It does not reallocate the rest of its queue or reschedule other copies of the post. Sending, already published, uncertain, or partly published deliveries cannot be dragged. YouTube releases and broadcasts continue to use their recording/stream scheduling workflows and conflict checks.

## Calendar imports

Streamctl sends its manual schedules to `PUT /api/v1/publishing/streams` during the existing broadcast-plan poll. This endpoint requires `recordings:write` and global admin access. Automatically imported recording plans are excluded to avoid duplicate calendar entries. Recurring streams expose their next systemd timer occurrence and schedule expression. Unavailable next-trigger times appear in the unscheduled tray. Manual stream entries are calendar projections; they are managed in streamctl. The existing conflict checker governs website recording/broadcast schedules; this snapshot does not reserve an unknown manual stream runtime or expand all future recurring occurrences.

Deploy the website endpoint before the streamer update. Account connections and worker activation are separate deployment steps. Local previews use sample accounts with publishing disabled.

## Validation

Run the normal Go suites for handlers, API, getters, X and YouTube. Run `BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=... go test ./internal/publishing` against an isolated migrated PostgreSQL database for concurrent allocation, rollback, stale edits, duplicate source references, multi-account rollback, independent destination moves, configured-slot restrictions, concurrent slot claims, timezone/DST behavior, and crash/reply recovery. Run streamctl's `go test ./internal/broadcastplans ./internal/btcppclient` with CGO enabled for SQLite. Use local fixtures for browser checks, with publishing disabled.

## Retiring Buffer

The running application has no Buffer client, API key setting, polling, calendar import, or posting fallback. Existing database rows are preserved as historical records and excluded from active channels, calendars and queue allocation. The old provider value and recording reference key are retained solely for historical compatibility. No schema migration deletes these records.

Before production activation:

1. Inventory the remaining scheduled posts in the old service and export their complete text, media, target accounts, and times. Run `ops/publishing-cutover.sql` to list the locally known historical posts and cached schedule. These local records are incomplete and are not proof of the old service's current queue or delivery outcome.
2. Reconcile each item as already published, intentionally canceled, or still needed. Stop/cancel remaining deliveries in the old service before scheduling replacements here. Merely deploying this code does not cancel an external queue or subscription.
3. Recreate needed X posts in the local queue and review their text, media, times and account. Hold Instagram/LinkedIn items until their direct integrations are implemented. Do not auto-replay the historical ledger: uncertain outcomes can otherwise produce duplicates.
4. For event announcements, keep their historical record until an operator confirms that the old pending delivery is canceled. Only then mark that record canceled to allow a new event announcement. Already published records remain intact.
5. Verify direct account connections and default slots, remove the retired API secret from deployment configuration, and enable the direct worker. Retire the old subscription after the outstanding queue has been reconciled.

This change itself makes no calls to Buffer, migrates no remote jobs, and changes no production subscription.
