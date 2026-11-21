# Shared publishing

`/admin/publishing` is the global-admin calendar for all events. It combines local social deliveries, recording/YouTube release times, X broadcast reservations, and streamer snapshots. Week, month, agenda, queue and account views share event, account, type and status filters. Dates are displayed in the selected IANA timezone.

## First direct publishers

- X: OAuth 2 with PKCE, encrypted database credentials, text, up to four JPEG/PNG/WebP images (5 MB each), and one optional follow-up reply. Video announcements use the recording/broadcast workflow; direct video attachments are not implemented in this version.
- YouTube and recorded X broadcasts: schedule and reschedule them in Publishing. **Schedule recordings** selects individual destinations, previews available slots, allows reordering, and applies the batch atomically. Use **Media library** for uploads and media metadata. Existing scheduling URLs redirect into Publishing; their legacy POST actions no longer change times.
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

Drag a scheduled post, YouTube release, or recorded broadcast (or its move handle) onto an available publication slot for that same account. Valid slots are highlighted during the drag. Other accounts, occupied slots, past times, and arbitrary calendar space are not valid destinations. The server validates the configured weekday/time in the account's timezone and serializes moves so two posts cannot claim one slot. Stale versions are rejected.

Click or keyboard-activate the move handle, or choose **Move to slot** in the item details or queue, to select from the next 90 days of available slots. This also works on mobile; touch-dragging uses the handle so scrolling the rest of the calendar still works. Escape cancels a drag.

A successful move pins only that delivery to the selected slot. It does not reallocate the rest of its queue or reschedule other copies of the post. Sending, already published, uncertain, or partly published deliveries cannot be dragged. YouTube releases and recorded broadcasts use the same slot picker and drag controls, with independent channel slots and runtime/conflict checks. Their changes remain pending until confirmed by the destination.

## Withholding a recording

In **Schedule recordings**, uncheck an item or use **Remove** in the preview to skip it for this batch. Removing an item recalculates the remaining slots. Use **Exclude from scheduling** for a persistent hold on that destination; **Select available** and future batches leave it out. Enable **Show excluded recordings** to restore it. Restoring makes it available without scheduling it. YouTube and broadcast exclusions are independent.

Exclusion does not change provider visibility and cannot cancel an existing or pending provider schedule. Resolve or clear that schedule first. YouTube's private visibility is shown separately because private videos can also be scheduled releases. Exclusions are checked by preview, individual moves, batch application, retries, and the provider worker. A stale preview cannot bypass a new exclusion.

## Recording schedules and provider synchronization

Migration 156 adds durable requested schedule changes and initializes separate broadcast slots from the existing cadence. Migration 157 adds a video-identity-bound YouTube visibility cache. Neither migration changes existing confirmed release/broadcast times or calls a provider. Historical items are not replayed.

A recording has independent `youtube:<id>` and `broadcast:<id>` calendar items. YouTube uses the existing YouTube slot table, including each slot's timezone; broadcasts have their own slots in **Channels & schedules**. The same calendar handles individual moves, keyboard/touch slot selection, and batch scheduling. Every save rechecks the item's revision, configured slots, runtime, and shared reservations under the existing database lock. A stale batch fails in full. Publication metadata submitted through the recording API is historical; future scheduling is managed here.

The recording picker defaults to newest events first, then Main, Talks, and Workshops within each event, following agenda start times within each room across all days. Other rooms follow those three; recordings without an agenda time follow timed recordings in their room, ordered by title. Drag rows or use the up/down buttons in the preview to override this default for the selected batch. Each move recalculates the preview; **Apply schedule** saves the reviewed times.

Schedule changes start as **pending**, then **syncing**. They update the actual YouTube schedule or existing X Studio broadcast through the established provider integration. The previously confirmed time remains reserved alongside the requested time until synchronization succeeds. A failed or interrupted operation stays visible for review; uncertain operations are not automatically repeated. Retrying an X create with no saved broadcast ID requires checking X Studio and explicitly confirming that no broadcast was created. If a broadcast does exist, leave the item stopped for reconciliation.

`PUBLISHING_ENABLED` gates both dispatchers. The recording dispatcher does not require direct X social-post OAuth configuration; YouTube and X Studio retain their own connection/update settings. Enabling the dispatcher applies pending provider schedule changes immediately, even when publication itself is weeks away. Review pending work before activation.

**Refresh recording status** fetches YouTube visibility and missing runtimes. Already public releases and live/ended broadcasts cannot move. Replacing a video invalidates its cached visibility by video identity.

To cancel a recording, click its calendar entry and choose **Cancel scheduled release** (YouTube) or **Cancel broadcast** (X). Confirm the destination in the prompt. YouTube cancellation removes the release time and sets the video to unlisted. X cancellation deletes the scheduled broadcast and disables its queued playback on the next streamer poll. Each action leaves the other destination unchanged and keeps the recording file.

Cancellation stays visible on the calendar, with its slot reserved, until the provider confirms it. Failures remain visible with **Retry cancellation**; uncertain results are also checked automatically without repeating the provider write. Public videos, live/ended broadcasts, operations already syncing, and releases less than a minute away cannot be canceled here. A queued broadcast that has never been sent to X can be canceled locally. Missing runtimes or source files do not prevent cancellation of an identified provider schedule.

Canceled recordings return to the unscheduled list. Scheduling an X broadcast again creates a fresh broadcast and link. The previous schedule is retained internally as a cancellation tombstone so the streamer can remove its timer, including after a delayed poll. Migration 170 enables these cancellation records without changing any existing schedules or contacting providers.

streamctl consumes confirmed X broadcast plans and cancellation requests. Uploads, encoding, playback, and manually configured recurring streamer jobs remain in their existing services; recording schedule management lives in Publishing.

## Calendar imports

Streamctl sends its manual schedules to `PUT /api/v1/publishing/streams` during the existing broadcast-plan poll. This endpoint requires `recordings:write` and global admin access. Automatically imported recording plans are excluded to avoid duplicate calendar entries. Recurring streams expose their next systemd timer occurrence and schedule expression. Unavailable next-trigger times appear in the unscheduled tray. Manual stream entries are calendar projections; they are managed in streamctl. The existing conflict checker governs website recording/broadcast schedules; this snapshot does not reserve an unknown manual stream runtime or expand all future recurring occurrences.

Deploy the website endpoint before the streamer update. Account connections and worker activation are separate deployment steps. Local previews use sample accounts with publishing disabled.

## Validation

Run the normal Go suites for handlers, API, getters, X and YouTube. Run `BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=... go test ./internal/publishing` against an isolated migrated PostgreSQL database for concurrent allocation, rollback, stale edits, duplicate source references, multi-account rollback, independent destination moves, configured-slot restrictions, concurrent slot claims, timezone/DST behavior, and crash/reply recovery. Recording tests also cover independent destinations, preserved confirmed reservations after provider failures, atomic batches, simultaneous moves, runtime and live-state restrictions, clearing a release, and interrupted synchronization. Run streamctl's `go test ./internal/broadcastplans ./internal/btcppclient` with CGO enabled for SQLite. Use local fixtures for browser checks, with publishing disabled.

## Retiring Buffer

The running application has no Buffer client, API key setting, polling, calendar import, or posting fallback. Existing database rows are preserved as historical records and excluded from active channels, calendars and queue allocation. The old provider value and recording reference key are retained solely for historical compatibility. No schema migration deletes these records.

Before production activation:

1. Inventory the remaining scheduled posts in the old service and export their complete text, media, target accounts, and times. Run `ops/publishing-cutover.sql` to list the locally known historical posts and cached schedule. These local records are incomplete and are not proof of the old service's current queue or delivery outcome.
2. Reconcile each item as already published, intentionally canceled, or still needed. Stop/cancel remaining deliveries in the old service before scheduling replacements here. Merely deploying this code does not cancel an external queue or subscription.
3. Recreate needed X posts in the local queue and review their text, media, times and account. Hold Instagram/LinkedIn items until their direct integrations are implemented. Do not auto-replay the historical ledger: uncertain outcomes can otherwise produce duplicates.
4. For event announcements, keep their historical record until an operator confirms that the old pending delivery is canceled. Only then mark that record canceled to allow a new event announcement. Already published records remain intact.
5. Verify direct account connections and default slots, remove the retired API secret from deployment configuration, and enable the direct worker. Retire the old subscription after the outstanding queue has been reconciled.

This change itself makes no calls to Buffer, migrates no remote jobs, and changes no production subscription.
